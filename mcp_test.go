package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// End-to-end: a real PocketBase app on a temp dir, the real router (so the
// auth middleware runs), the MCP endpoint served over HTTP, and the SDK client.

type mcpEnv struct {
	app *pocketbase.PocketBase
	srv *httptest.Server
}

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir(), HideStartBanner: true})
	registerHooks(app)
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAllMigrations(); err != nil {
		t.Fatal(err)
	}
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	registerMCP(app, &core.ServeEvent{App: app, Router: router})
	// same catch-all as main.go so route conflicts show up here, not at boot
	router.GET("/{path...}", func(re *core.RequestEvent) error { return re.String(http.StatusOK, "ui") })
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		_ = app.ResetBootstrapState()
	})
	return &mcpEnv{app: app, srv: srv}
}

func (e *mcpEnv) user(t *testing.T, email string) *core.Record {
	t.Helper()
	col, err := e.app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	u := core.NewRecord(col)
	u.SetEmail(email)
	u.SetPassword("password123")
	if err := e.app.Save(u); err != nil {
		t.Fatal(err)
	}
	return u
}

func staticToken(t *testing.T, u *core.Record) string {
	t.Helper()
	tok, err := u.NewStaticAuthToken(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if a.token != "" {
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (e *mcpEnv) connect(token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	tr := &mcp.StreamableClientTransport{
		Endpoint:   e.srv.URL + mcpPath,
		HTTPClient: &http.Client{Transport: authTransport{token}},
		MaxRetries: -1,
	}
	return client.Connect(context.Background(), tr, nil)
}

func (e *mcpEnv) session(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	cs, err := e.connect(token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func call[T any](t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, resultText(res))
	}
	var out T
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: decode %s: %v", name, b, err)
	}
	return out
}

func callErr(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("%s: expected a tool error, got %s", name, resultText(res))
	}
	return resultText(res)
}

func TestMCPRequiresAuth(t *testing.T) {
	env := newMCPEnv(t)
	if _, err := env.connect(""); err == nil {
		t.Fatal("connected without a token")
	}
	if _, err := env.connect("not-a-token"); err == nil {
		t.Fatal("connected with a bogus token")
	}
	// superusers are not users: no data to scope to
	su, err := env.app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	admin := core.NewRecord(su)
	admin.SetEmail("admin@example.com")
	admin.SetPassword("password123")
	if err := env.app.Save(admin); err != nil {
		t.Fatal(err)
	}
	if _, err := env.connect(staticToken(t, admin)); err == nil {
		t.Fatal("connected with a superuser token")
	}
}

func TestMCPTransactions(t *testing.T) {
	env := newMCPEnv(t)
	u := env.user(t, "a@example.com")
	cs := env.session(t, staticToken(t, u))

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) < 15 {
		t.Fatalf("expected the full tool set, got %d", len(tools.Tools))
	}

	tx := call[txOut](t, cs, "create_transaction", map[string]any{
		"type": "expense", "amount": 120, "date": "2025-03-10", "category": "Software", "tags": []string{"acme"}, "note": "IDE licence",
	})
	if tx.Category != "Software" || len(tx.Tags) != 1 || tx.Tags[0] != "acme" || tx.Area != "business" || tx.Date != "2025-03-10" {
		t.Fatalf("unexpected transaction: %+v", tx)
	}
	call[txOut](t, cs, "create_transaction", map[string]any{"type": "income", "amount": 1000, "date": "2025-06-01", "area": "rental"})
	call[txOut](t, cs, "create_transaction", map[string]any{"type": "expense", "amount": 5, "date": "2024-12-31"})

	// invalid input is a tool error, not a protocol error
	if msg := callErr(t, cs, "create_transaction", map[string]any{"type": "gift", "amount": 1}); !strings.Contains(msg, "type must be one of") {
		t.Fatalf("unexpected error text: %s", msg)
	}

	list := call[listTxOutput](t, cs, "list_transactions", map[string]any{"year": 2025})
	if len(list.Items) != 2 || list.HasMore || list.Items[0].Date != "2025-06-01" {
		t.Fatalf("year filter: %+v", list)
	}
	if got := call[listTxOutput](t, cs, "list_transactions", map[string]any{"category": "software"}); len(got.Items) != 1 {
		t.Fatalf("case-insensitive category filter: %+v", got)
	}
	if got := call[listTxOutput](t, cs, "list_transactions", map[string]any{"tag": "ACME", "search": "licence"}); len(got.Items) != 1 {
		t.Fatalf("tag + search filter: %+v", got)
	}
	if got := call[listTxOutput](t, cs, "list_transactions", map[string]any{"limit": 1}); len(got.Items) != 1 || !got.HasMore {
		t.Fatalf("pagination: %+v", got)
	}

	sum := call[summaryOutput](t, cs, "year_summary", map[string]any{"year": 2025})
	if sum.Income != 1000 || sum.Expenses != 120 || sum.Net != 880 || sum.Transactions != 2 {
		t.Fatalf("totals: %+v", sum)
	}
	if sum.Area["rental"].Income != 1000 || sum.Category["Software"].Expenses != 120 || sum.Category["(none)"].Income != 1000 || sum.Tag["acme"].Net != -120 {
		t.Fatalf("buckets: area=%+v cat=%+v tag=%+v", sum.Area["rental"], sum.Category, sum.Tag)
	}

	cats := call[labelsOutput](t, cs, "list_categories", nil)
	if len(cats.Items) != 1 || cats.Items[0].Name != "Software" || cats.Items[0].Transactions != 1 {
		t.Fatalf("categories: %+v", cats)
	}

	upd := call[txOut](t, cs, "update_transaction", map[string]any{"id": tx.ID, "amount": 100, "tags": []string{}, "category": ""})
	if upd.Amount != 100 || len(upd.Tags) != 0 || upd.Category != "" {
		t.Fatalf("update: %+v", upd)
	}
	if got := call[txOut](t, cs, "get_transaction", map[string]any{"id": tx.ID}); got.Amount != 100 || got.Note != "IDE licence" {
		t.Fatalf("get after update: %+v", got)
	}

	call[deletedOutput](t, cs, "delete_transaction", map[string]any{"id": tx.ID})
	if msg := callErr(t, cs, "get_transaction", map[string]any{"id": tx.ID}); !strings.Contains(msg, "not found") {
		t.Fatalf("deleted transaction still readable: %s", msg)
	}
}

func TestMCPOwnership(t *testing.T) {
	env := newMCPEnv(t)
	a, b := env.user(t, "a@example.com"), env.user(t, "b@example.com")
	csA, csB := env.session(t, staticToken(t, a)), env.session(t, staticToken(t, b))

	tx := call[txOut](t, csA, "create_transaction", map[string]any{"type": "expense", "amount": 42, "category": "Secret"})
	loan := call[loanOut](t, csA, "create_loan", map[string]any{"name": "Car", "principal": 10000})

	if got := call[listTxOutput](t, csB, "list_transactions", nil); len(got.Items) != 0 {
		t.Fatalf("user b sees user a's transactions: %+v", got)
	}
	if got := call[labelsOutput](t, csB, "list_categories", nil); len(got.Items) != 0 {
		t.Fatalf("user b sees user a's categories: %+v", got)
	}
	callErr(t, csB, "get_transaction", map[string]any{"id": tx.ID})
	callErr(t, csB, "update_transaction", map[string]any{"id": tx.ID, "amount": 1})
	callErr(t, csB, "delete_transaction", map[string]any{"id": tx.ID})
	// linking a foreign loan is refused as well
	callErr(t, csB, "create_transaction", map[string]any{"type": "expense", "amount": 1, "loan_id": loan.ID})

	if got := call[txOut](t, csA, "get_transaction", map[string]any{"id": tx.ID}); got.Amount != 42 {
		t.Fatalf("user a's transaction was modified: %+v", got)
	}
}

func TestMCPRecurringAndLoans(t *testing.T) {
	env := newMCPEnv(t)
	u := env.user(t, "a@example.com")
	cs := env.session(t, staticToken(t, u))

	start := today().AddDate(0, -2, 0).Format(dateLayout)
	rec := call[createRecurringOutput](t, cs, "create_recurring", map[string]any{
		"type": "expense", "amount": 900, "interval": "monthly", "start": start, "area": "private", "category": "Rent",
	})
	if rec.Generated != 3 || rec.Next == "" || !rec.Active || rec.LastGenerated == "" {
		t.Fatalf("create_recurring: %+v", rec)
	}
	if got := call[listTxOutput](t, cs, "list_transactions", nil); len(got.Items) != 3 || got.Items[0].RecurringID != rec.ID || got.Items[0].Category != "Rent" {
		t.Fatalf("backfilled transactions: %+v", got)
	}
	paused := call[recurringOut](t, cs, "update_recurring", map[string]any{"id": rec.ID, "active": false})
	if paused.Active || paused.Next != "" {
		t.Fatalf("pause: %+v", paused)
	}
	if got := call[recurringListOutput](t, cs, "list_recurring", nil); len(got.Items) != 1 {
		t.Fatalf("list_recurring: %+v", got)
	}
	call[deletedOutput](t, cs, "delete_recurring", map[string]any{"id": rec.ID})
	if n, _ := env.app.CountRecords("transactions", dbx.HashExp{"user": u.Id}); n != 3 {
		t.Fatalf("deleting the template removed transactions: %d left", n)
	}

	loan := call[loanOut](t, cs, "create_loan", map[string]any{"name": "Car", "principal": 10000, "interest_rate": 4, "start": "2025-01-01"})
	call[txOut](t, cs, "create_transaction", map[string]any{"type": "expense", "amount": 500, "loan_id": loan.ID, "loan_interest": 30})
	call[txOut](t, cs, "create_transaction", map[string]any{"type": "expense", "amount": 500, "loan_id": loan.ID, "loan_interest": 28})
	loans := call[loansOutput](t, cs, "list_loans", nil)
	if len(loans.Items) != 1 {
		t.Fatalf("list_loans: %+v", loans)
	}
	l := loans.Items[0]
	if l.Payments != 2 || l.InterestPaid != 58 || l.Repaid != 942 || l.Remaining != 9058 {
		t.Fatalf("loan math: %+v", l)
	}
	if got := call[listTxOutput](t, cs, "list_transactions", map[string]any{"loan_id": loan.ID}); len(got.Items) != 2 {
		t.Fatalf("loan filter: %+v", got)
	}

	// the users hook created the default rule
	rule := call[taxRuleOutput](t, cs, "get_tax_rule", nil)
	if rule.Name == "" || !strings.Contains(rule.Script, "d.area.business") {
		t.Fatalf("get_tax_rule: %+v", rule)
	}
}

func TestTokenEndpoints(t *testing.T) {
	env := newMCPEnv(t)
	u := env.user(t, "a@example.com")
	sessionToken, err := u.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	post := func(path, token string, body any) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(http.MethodPost, env.srv.URL+path, &buf)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	if res := post("/api/murmel/tokens", "", map[string]any{"days": 30}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("minting without auth: %d", res.StatusCode)
	}
	res := post("/api/murmel/tokens", sessionToken, map[string]any{"days": 30})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("mint: %d", res.StatusCode)
	}
	var minted struct{ Token, Expires string }
	if err := json.NewDecoder(res.Body).Decode(&minted); err != nil || minted.Token == "" {
		t.Fatalf("mint body: %v %+v", err, minted)
	}
	exp, err := time.Parse(time.RFC3339, minted.Expires)
	if err != nil || exp.Before(time.Now().Add(29*24*time.Hour)) || exp.After(time.Now().Add(31*24*time.Hour)) {
		t.Fatalf("expiry: %v %v", minted.Expires, err)
	}

	cs := env.session(t, minted.Token)
	call[listTxOutput](t, cs, "list_transactions", nil)

	if res := post("/api/murmel/tokens/revoke", sessionToken, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if _, err := env.connect(minted.Token); err == nil {
		t.Fatal("static token still valid after revoke")
	}
	if _, err := env.connect(sessionToken); err == nil {
		t.Fatal("session token still valid after revoke")
	}
}

func TestReadOnlyToken(t *testing.T) {
	env := newMCPEnv(t)
	u := env.user(t, "a@example.com")
	full := env.session(t, staticToken(t, u))
	tx := call[txOut](t, full, "create_transaction", map[string]any{"type": "expense", "amount": 10, "category": "Software"})

	sessionToken, err := u.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path, token string, body any) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, env.srv.URL+path, &buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := do(http.MethodPost, "/api/murmel/tokens", sessionToken, map[string]any{"days": 30, "scope": "read"})
	var minted struct{ Token, Scope string }
	if err := json.NewDecoder(res.Body).Decode(&minted); err != nil || minted.Scope != "read" || minted.Token == "" {
		t.Fatalf("mint read-only: %d %v %+v", res.StatusCode, err, minted)
	}

	cs := env.session(t, minted.Token)
	if init := cs.InitializeResult(); !strings.Contains(init.Instructions, "READ-ONLY") || !strings.Contains(init.Instructions, "Read & write") {
		t.Fatalf("instructions do not explain the read-only token: %q", init.Instructions)
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 8 {
		t.Fatalf("read-only tool set: %d tools", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if writeTools[tool.Name] || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("write tool advertised to a read-only session: %s", tool.Name)
		}
	}
	// reads work, writes are refused with the explanation
	if got := call[listTxOutput](t, cs, "list_transactions", nil); len(got.Items) != 1 {
		t.Fatalf("read-only list: %+v", got)
	}
	call[summaryOutput](t, cs, "year_summary", nil)
	for name, args := range map[string]map[string]any{
		"create_transaction": {"type": "expense", "amount": 1},
		"update_transaction": {"id": tx.ID, "amount": 999},
		"delete_transaction": {"id": tx.ID},
		"create_loan":        {"name": "x", "principal": 1},
	} {
		if msg := callErr(t, cs, name, args); !strings.Contains(msg, "READ-ONLY") || !strings.Contains(msg, "#/connect") {
			t.Fatalf("%s: unexpected error text: %s", name, msg)
		}
	}
	if got := call[txOut](t, full, "get_transaction", map[string]any{"id": tx.ID}); got.Amount != 10 {
		t.Fatalf("read-only session modified data: %+v", got)
	}

	// the REST API honors the scope too
	if res := do(http.MethodGet, "/api/collections/transactions/records", minted.Token, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("REST read with read-only token: %d", res.StatusCode)
	}
	if res := do(http.MethodPost, "/api/collections/transactions/records", minted.Token, map[string]any{"user": u.Id, "type": "expense", "amount": 1, "date": "2025-01-01", "area": "business"}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("REST write with read-only token: %d", res.StatusCode)
	}
	if res := do(http.MethodPatch, "/api/collections/transactions/records/"+tx.ID, minted.Token, map[string]any{"amount": 999}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("REST update with read-only token: %d", res.StatusCode)
	}
	if res := do(http.MethodPost, "/api/murmel/tokens", minted.Token, map[string]any{"days": 1}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only token could mint a token: %d", res.StatusCode)
	}
	if res := do(http.MethodPost, "/api/murmel/tokens/revoke", minted.Token, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only token could revoke: %d", res.StatusCode)
	}
	if n, _ := env.app.CountRecords("transactions", dbx.HashExp{"user": u.Id}); n != 1 {
		t.Fatalf("read-only token created records: %d", n)
	}
}

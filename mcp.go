// MCP (Model Context Protocol) server so AI agents can work with a user's data.
//
// Mounted at /api/murmel/mcp as a stateless streamable-HTTP endpoint. Every
// request must carry a PocketBase *user* auth token in the Authorization
// header — normally a long-lived static token minted at POST /api/murmel/tokens
// (the "AI & API" page in the UI). All tools are scoped to that user; there is
// no way to reach another user's records through this endpoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	mcpPath          = "/api/murmel/mcp"
	dateLayout       = "2006-01-02"
	defaultTokenDays = 365
	maxTokenDays     = 3650

	// scopeClaim marks a read-only token. Regular PocketBase tokens have no
	// scope claim and are read/write.
	scopeClaim = "scope"
	scopeRead  = "read"
)

// readOnlyMsg is what an agent sees when it holds a read-only token: in the
// server instructions at connect time and as the error of any write attempt.
const readOnlyMsg = "This connection uses a READ-ONLY token: nothing can be created, changed or deleted, and only the read tools are available. " +
	"If the user wants changes made, ask them to create a token with \"Read & write\" access on the AI & API page of the murmelmoney web app (#/connect) and reconnect this MCP server with it."

// Request-context keys: the authenticated user record and the token scope,
// handed from the PocketBase middleware to the tool handlers (the stateless
// transport derives the tool context from the HTTP request context).
type (
	mcpAuthKey  struct{}
	mcpScopeKey struct{}
)

const mcpInstructions = `murmelmoney is a personal finance tracker (incomes and expenses, receipts, a rough yearly tax estimate).
Amounts are gross EUR and always positive; the sign is implied by type (income or expense).
Every transaction belongs to an area: business (freelance work), rental (a let-out property) or private.
A transaction has at most one category (what kind: Software, Honorarnote, Repairs, ...) and any number of tags
(what it belongs to: a client, a project, a cost point, the house, ...). Prefer names that already exist
(list_categories / list_tags) over inventing new ones; names are matched case-insensitively.
Dates are YYYY-MM-DD. Recurring templates materialize real transactions automatically (list_recurring shows the
next occurrence); their interval is weekly/monthly/quarterly/half-yearly/yearly or "<n> weeks|months|years"; loan payments are expense transactions linked to a loan via loan_id, with an optional interest part.
year_summary aggregates a year like the app's overview page; get_tax_rule returns the user's own tax-estimate
script (a JavaScript function body over that summary) if they ask what to set aside for tax.
For imports use create_transactions (a list, all-or-nothing, optional skip_duplicates) and tag_transactions
(add/remove tags on many rows at once). In update tools, "none" clears an optional field (same as an empty
string, which some clients drop): category, loan_id, end, start; tags: ["none"] clears the tags.`

// registerMCP mounts the MCP endpoint and the personal-access-token endpoints.
func registerMCP(app core.App, e *core.ServeEvent) {
	cache := mcp.NewSchemaCache()
	full, readOnly := newMCPServer(app, false, cache), newMCPServer(app, true, cache)
	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			if r.Context().Value(mcpScopeKey{}) == scopeRead {
				return readOnly
			}
			return full
		},
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)

	// Read-only tokens are ordinary user tokens for PocketBase, so the REST API
	// must refuse writes with them too — everything but GET/HEAD/OPTIONS, except
	// the MCP endpoint (JSON-RPC over POST), where the read-only server and the
	// write handlers enforce the scope.
	e.Router.BindFunc(func(re *core.RequestEvent) error {
		if re.Auth == nil || tokenScope(re.Request) != scopeRead {
			return re.Next()
		}
		switch re.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return re.Next()
		}
		if re.Request.URL.Path == mcpPath {
			return re.Next()
		}
		return re.ForbiddenError("This token is read-only.", nil)
	})

	serve := func(re *core.RequestEvent) error {
		ctx := context.WithValue(re.Request.Context(), mcpAuthKey{}, re.Auth)
		ctx = context.WithValue(ctx, mcpScopeKey{}, tokenScope(re.Request))
		handler.ServeHTTP(re.Response, re.Request.WithContext(ctx))
		return nil
	}
	// Explicit methods (not Any): a method-less pattern would conflict with the
	// "GET /{path...}" UI catch-all in Go's ServeMux. GET/DELETE answer 405 in
	// stateless mode, which is what clients probing for a session expect.
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		e.Router.Route(method, mcpPath, serve).Bind(apis.RequireAuth("users"))
	}

	// Mint a static (non-refreshable, long-lived) auth token for the current
	// user; scope "read" makes it read-only.
	e.Router.POST("/api/murmel/tokens", func(re *core.RequestEvent) error {
		var body struct {
			Days  int    `json:"days"`
			Scope string `json:"scope"`
		}
		if err := re.BindBody(&body); err != nil {
			return re.BadRequestError("Invalid request body.", err)
		}
		if body.Days <= 0 {
			body.Days = defaultTokenDays
		}
		if body.Days > maxTokenDays {
			body.Days = maxTokenDays
		}
		d := time.Duration(body.Days) * 24 * time.Hour
		var token string
		var err error
		if body.Scope == scopeRead {
			token, err = newReadOnlyToken(re.Auth, d)
		} else {
			body.Scope = "write"
			token, err = re.Auth.NewStaticAuthToken(d)
		}
		if err != nil {
			return err
		}
		return re.JSON(http.StatusOK, map[string]any{
			"token":   token,
			"scope":   body.Scope,
			"expires": time.Now().Add(d).UTC().Format(time.RFC3339),
		})
	}).Bind(apis.RequireAuth("users"))

	// Rotating the user's token key invalidates every token ever issued for
	// them — static tokens and browser sessions alike.
	e.Router.POST("/api/murmel/tokens/revoke", func(re *core.RequestEvent) error {
		re.Auth.RefreshTokenKey()
		if err := re.App.Save(re.Auth); err != nil {
			return err
		}
		return re.NoContent(http.StatusNoContent)
	}).Bind(apis.RequireAuth("users"))
}

// newReadOnlyToken mints a static auth token like Record.NewStaticAuthToken,
// plus a scope claim that the REST middleware and the MCP server honor.
// PocketBase itself ignores unknown claims, so the token authenticates normally.
func newReadOnlyToken(user *core.Record, d time.Duration) (string, error) {
	col := user.Collection()
	if !col.IsAuth() {
		return "", core.ErrNotAuthRecord
	}
	key := user.TokenKey() + col.AuthToken.Secret
	if key == "" {
		return "", core.ErrMissingSigningKey
	}
	return security.NewJWT(jwt.MapClaims{
		core.TokenClaimType:         core.TokenTypeAuth,
		core.TokenClaimId:           user.Id,
		core.TokenClaimCollectionId: col.Id,
		core.TokenClaimRefreshable:  false,
		scopeClaim:                  scopeRead,
	}, key, d)
}

// tokenScope reads the scope claim of the request's bearer token. The
// signature was already verified by PocketBase's auth middleware (a request
// with a tampered token never has e.Auth set), so an unverified parse is fine.
func tokenScope(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && strings.EqualFold(token[:7], "Bearer ") {
		token = token[7:]
	}
	if token == "" {
		return ""
	}
	claims, err := security.ParseUnverifiedJWT(token)
	if err != nil {
		return ""
	}
	scope, _ := claims[scopeClaim].(string)
	return scope
}

// writeTools are the tool names that modify data; the read-only server does
// not advertise them and answers calls to them with readOnlyMsg.
var writeTools = map[string]bool{
	"create_transaction": true, "create_transactions": true, "update_transaction": true, "delete_transaction": true,
	"tag_transactions": true, "rename_label": true, "delete_label": true,
	"create_recurring": true, "update_recurring": true, "delete_recurring": true,
	"create_loan": true, "update_loan": true, "delete_loan": true,
}

// newMCPServer builds the tool set. The read-only variant registers only the
// read tools, says so in its instructions and turns a call to a write tool
// into a clear tool error rather than "tool not found".
func newMCPServer(app core.App, readOnly bool, cache *mcp.SchemaCache) *mcp.Server {
	instructions := mcpInstructions
	if readOnly {
		instructions += "\n\n" + readOnlyMsg
	}
	s := mcp.NewServer(
		&mcp.Implementation{Name: "murmelmoney", Version: version},
		&mcp.ServerOptions{Instructions: instructions, SchemaCache: cache},
	)
	if readOnly {
		s.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && method == "tools/call" && writeTools[p.Name] {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: readOnlyMsg}}}, nil
				}
				return next(ctx, method, req)
			}
		})
	}
	t := &mcpTools{app: app}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	rw := &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false)}
	del := &mcp.ToolAnnotations{DestructiveHint: ptr(true)}

	mcp.AddTool(s, &mcp.Tool{Name: "list_transactions", Annotations: ro,
		Description: "List the user's transactions, newest first. Filter by year or a date range, type, area, category, tag, note text, exact amount or loan (date range + amount finds possible duplicates before an import)."}, t.listTransactions)
	mcp.AddTool(s, &mcp.Tool{Name: "get_transaction", Annotations: ro,
		Description: "Fetch one transaction by id, including the full note and attachment file names."}, t.getTransaction)
	mcp.AddTool(s, &mcp.Tool{Name: "list_categories", Annotations: ro,
		Description: "All categories of the user with how many transactions use each."}, t.listCategories)
	mcp.AddTool(s, &mcp.Tool{Name: "list_tags", Annotations: ro,
		Description: "All tags of the user with how many transactions use each."}, t.listTags)
	mcp.AddTool(s, &mcp.Tool{Name: "year_summary", Annotations: ro,
		Description: "Totals of a year: income, expenses and net overall, per area, per category and per tag, plus the still-planned recurring amounts (projected) for the rest of the year."}, t.yearSummary)
	mcp.AddTool(s, &mcp.Tool{Name: "get_tax_rule", Annotations: ro,
		Description: "The user's active tax-estimate rule: the body of a JavaScript function(d) that returns [{label, value, hint?}] lines, where d has the shape of year_summary (year, income, expenses, net, area, category, tag, projected) plus d.transactions (the year's transactions as {date, type, area, category, tags, amount}). Evaluate it yourself against a year_summary to estimate the tax."}, t.getTaxRule)
	mcp.AddTool(s, &mcp.Tool{Name: "list_recurring", Annotations: ro,
		Description: "Recurring transaction templates (rent, subscriptions, retainers) with their next occurrence."}, t.listRecurring)
	mcp.AddTool(s, &mcp.Tool{Name: "list_loans", Annotations: ro,
		Description: "Loans with principal, what has been repaid, interest paid and the remaining balance (principal minus repayments excluding interest)."}, t.listLoans)
	if readOnly {
		return s
	}

	mcp.AddTool(s, &mcp.Tool{Name: "create_transaction", Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false)},
		Description: "Record an income or expense. Category and tags are given by name and created when they do not exist yet."}, t.createTransaction)
	mcp.AddTool(s, &mcp.Tool{Name: "create_transactions", Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false)},
		Description: "Record many transactions in one call (max 200), all-or-nothing: if one item is invalid nothing is created. With skip_duplicates, items that already exist (same date, type, amount and area) are skipped and reported, so an import can be re-run safely."}, t.createTransactions)
	mcp.AddTool(s, &mcp.Tool{Name: "update_transaction", Annotations: rw,
		Description: "Change fields of an existing transaction. Only the given fields change. 'none' (or an empty string) clears category or loan_id; tags replaces all tags (['none'] clears them) while add_tags/remove_tags change them incrementally."}, t.updateTransaction)
	mcp.AddTool(s, &mcp.Tool{Name: "tag_transactions", Annotations: rw,
		Description: "Add and/or remove tags on many transactions at once (max 500 ids) without touching their other tags."}, t.tagTransactions)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_transaction", Annotations: del,
		Description: "Delete a transaction (and its attachments) permanently."}, t.deleteTransaction)
	mcp.AddTool(s, &mcp.Tool{Name: "rename_label", Annotations: rw,
		Description: "Rename a category or tag. Renaming onto a name that already exists merges the two: every transaction and recurring template is moved to the existing one."}, t.renameLabel)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_label", Annotations: del,
		Description: "Delete a category or tag. Transactions and templates that used it keep existing without it."}, t.deleteLabel)
	mcp.AddTool(s, &mcp.Tool{Name: "create_recurring", Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false)},
		Description: "Create a recurring template. Occurrences are start + n*interval (day-of-month clamped, weekly keeps the weekday); past occurrences up to today are generated immediately as real transactions."}, t.createRecurring)
	mcp.AddTool(s, &mcp.Tool{Name: "update_recurring", Annotations: rw,
		Description: "Change a recurring template (e.g. pause it with active=false, set an end date, change the amount). Only future occurrences are affected."}, t.updateRecurring)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_recurring", Annotations: del,
		Description: "Delete a recurring template. Transactions it already generated are kept."}, t.deleteRecurring)
	mcp.AddTool(s, &mcp.Tool{Name: "create_loan", Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false)},
		Description: "Create a loan. Payments are then recorded as expense transactions with loan_id (and an optional loan_interest part)."}, t.createLoan)
	mcp.AddTool(s, &mcp.Tool{Name: "update_loan", Annotations: rw,
		Description: "Change a loan: name, principal, interest rate, start date, note, or close/reopen it with closed. Only the given fields change."}, t.updateLoan)
	mcp.AddTool(s, &mcp.Tool{Name: "delete_loan", Annotations: del,
		Description: "Delete a loan. Its payment transactions are kept, just no longer linked to a loan."}, t.deleteLoan)
	return s
}

// ---------------------------------------------------------------------------

type mcpTools struct{ app core.App }

func authUser(ctx context.Context) (*core.Record, error) {
	u, _ := ctx.Value(mcpAuthKey{}).(*core.Record)
	if u == nil {
		return nil, errors.New("not authenticated")
	}
	return u, nil
}

// writeUser is authUser for tools that modify data. Read-only sessions never
// reach these handlers (see newMCPServer), but they check the scope anyway.
func writeUser(ctx context.Context) (*core.Record, error) {
	if ctx.Value(mcpScopeKey{}) == scopeRead {
		return nil, errors.New(readOnlyMsg)
	}
	return authUser(ctx)
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", s)
	}
	return t.UTC(), nil
}

func dateOut(dt types.DateTime) string {
	if dt.IsZero() {
		return ""
	}
	return dt.Time().UTC().Format(dateLayout)
}

func today() time.Time { return time.Now().UTC().Truncate(24 * time.Hour) }

// cleared reports whether an optional string in an update means "unset".
// Some MCP clients drop empty strings from arguments, so "none" works too.
func cleared(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "null":
		return true
	}
	return false
}

func oneOf(field, v string, allowed ...string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s", field, strings.Join(allowed, ", "))
}

// owned loads a record of the given collection and checks it belongs to the user.
func (m *mcpTools) owned(uid, col, id string) (*core.Record, error) {
	rec, err := m.app.FindRecordById(col, strings.TrimSpace(id))
	if err != nil || rec.GetString("user") != uid {
		return nil, fmt.Errorf("%s %q not found", strings.TrimSuffix(col, "s"), id)
	}
	return rec, nil
}

// labels returns id→name for the user's categories or tags.
func (m *mcpTools) labels(uid, col string) (map[string]string, error) {
	recs, err := m.app.FindRecordsByFilter(col, "user = {:uid}", "name", 0, 0, dbx.Params{"uid": uid})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(recs))
	for _, r := range recs {
		out[r.Id] = r.GetString("name")
	}
	return out, nil
}

func findLabel(labels map[string]string, name string) string {
	for id, n := range labels {
		if strings.EqualFold(n, name) {
			return id
		}
	}
	return ""
}

// ensureLabel finds a category/tag by name (case-insensitive) or creates it.
func (m *mcpTools) ensureLabel(uid, col, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	labels, err := m.labels(uid, col)
	if err != nil {
		return "", err
	}
	if id := findLabel(labels, name); id != "" {
		return id, nil
	}
	c, err := m.app.FindCollectionByNameOrId(col)
	if err != nil {
		return "", err
	}
	r := core.NewRecord(c)
	r.Set("user", uid)
	r.Set("name", name)
	if err := m.app.Save(r); err != nil {
		return "", err
	}
	return r.Id, nil
}

// ensureTags resolves tag names to ids, creating unknown tags. A single
// "none" entry means "no tags" (for clients that drop empty lists).
func (m *mcpTools) ensureTags(uid string, names []string) ([]string, error) {
	if len(names) == 1 && cleared(names[0]) {
		return []string{}, nil
	}
	ids := make([]string, 0, len(names))
	for _, n := range names {
		id, err := m.ensureLabel(uid, "tags", n)
		if err != nil {
			return nil, err
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// --- transactions ------------------------------------------------------------

type txOut struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Date         string   `json:"date"`
	Amount       float64  `json:"amount"`
	Area         string   `json:"area"`
	Category     string   `json:"category,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Note         string   `json:"note,omitempty"`
	Attachments  []string `json:"attachments,omitempty" jsonschema:"file names of attached receipts/invoices"`
	RecurringID  string   `json:"recurring_id,omitempty" jsonschema:"id of the recurring template that generated this transaction"`
	LoanID       string   `json:"loan_id,omitempty"`
	LoanInterest float64  `json:"loan_interest,omitempty"`
}

func txToOut(r *core.Record, cats, tags map[string]string) txOut {
	o := txOut{
		ID:           r.Id,
		Type:         r.GetString("type"),
		Date:         dateOut(r.GetDateTime("date")),
		Amount:       r.GetFloat("amount"),
		Area:         r.GetString("area"),
		Category:     cats[r.GetString("category")],
		Note:         r.GetString("note"),
		Attachments:  r.GetStringSlice("attachments"),
		RecurringID:  r.GetString("recurring"),
		LoanID:       r.GetString("loan"),
		LoanInterest: r.GetFloat("loan_interest"),
	}
	for _, id := range r.GetStringSlice("tags") {
		if n, ok := tags[id]; ok {
			o.Tags = append(o.Tags, n)
		}
	}
	return o
}

type listTxInput struct {
	Year     int     `json:"year,omitempty" jsonschema:"calendar year; ignored when from/to are given"`
	From     string  `json:"from,omitempty" jsonschema:"first date (inclusive), YYYY-MM-DD"`
	To       string  `json:"to,omitempty" jsonschema:"last date (inclusive), YYYY-MM-DD"`
	Type     string  `json:"type,omitempty" jsonschema:"income or expense"`
	Area     string  `json:"area,omitempty" jsonschema:"business, rental or private"`
	Category string  `json:"category,omitempty" jsonschema:"category name"`
	Tag      string  `json:"tag,omitempty" jsonschema:"tag name"`
	Search   string  `json:"search,omitempty" jsonschema:"text contained in the note"`
	Amount   float64 `json:"amount,omitempty" jsonschema:"exact amount (within a cent)"`
	LoanID   string  `json:"loan_id,omitempty" jsonschema:"only payments of this loan"`
	Limit    int     `json:"limit,omitempty" jsonschema:"max rows, default 100, max 500"`
	Offset   int     `json:"offset,omitempty"`
}

type listTxOutput struct {
	Items   []txOut `json:"items"`
	HasMore bool    `json:"has_more" jsonschema:"true when more rows exist beyond limit/offset"`
}

func (m *mcpTools) listTransactions(ctx context.Context, _ *mcp.CallToolRequest, in listTxInput) (*mcp.CallToolResult, listTxOutput, error) {
	var out listTxOutput
	u, err := authUser(ctx)
	if err != nil {
		return nil, out, err
	}
	cats, err := m.labels(u.Id, "categories")
	if err != nil {
		return nil, out, err
	}
	tags, err := m.labels(u.Id, "tags")
	if err != nil {
		return nil, out, err
	}

	where := []string{"user = {:uid}"}
	params := dbx.Params{"uid": u.Id}
	if in.Year > 0 && in.From == "" && in.To == "" {
		in.From = fmt.Sprintf("%d-01-01", in.Year)
		in.To = fmt.Sprintf("%d-12-31", in.Year)
	}
	if in.From != "" {
		t, err := parseDate(in.From)
		if err != nil {
			return nil, out, err
		}
		where = append(where, "date >= {:from}")
		params["from"] = t.Format(types.DefaultDateLayout)
	}
	if in.To != "" {
		t, err := parseDate(in.To)
		if err != nil {
			return nil, out, err
		}
		where = append(where, "date < {:to}")
		params["to"] = t.AddDate(0, 0, 1).Format(types.DefaultDateLayout)
	}
	if in.Type != "" {
		if err := oneOf("type", in.Type, "income", "expense"); err != nil {
			return nil, out, err
		}
		where = append(where, "type = {:type}")
		params["type"] = in.Type
	}
	if in.Area != "" {
		if err := oneOf("area", in.Area, "business", "rental", "private"); err != nil {
			return nil, out, err
		}
		where = append(where, "area = {:area}")
		params["area"] = in.Area
	}
	if in.Category != "" {
		id := findLabel(cats, in.Category)
		if id == "" {
			return nil, listTxOutput{Items: []txOut{}}, nil
		}
		where = append(where, "category = {:cat}")
		params["cat"] = id
	}
	if in.Tag != "" {
		id := findLabel(tags, in.Tag)
		if id == "" {
			return nil, listTxOutput{Items: []txOut{}}, nil
		}
		where = append(where, "tags ~ {:tag}")
		params["tag"] = id
	}
	if q := strings.TrimSpace(in.Search); q != "" {
		where = append(where, "note ~ {:q}")
		params["q"] = q
	}
	if in.Amount != 0 {
		where = append(where, "amount > {:amin} && amount < {:amax}")
		params["amin"], params["amax"] = in.Amount-0.005, in.Amount+0.005
	}
	if in.LoanID != "" {
		where = append(where, "loan = {:loan}")
		params["loan"] = in.LoanID
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	recs, err := m.app.FindRecordsByFilter("transactions", strings.Join(where, " && "), "-date,-created", limit+1, max(in.Offset, 0), params)
	if err != nil {
		return nil, out, err
	}
	if len(recs) > limit {
		out.HasMore = true
		recs = recs[:limit]
	}
	out.Items = make([]txOut, 0, len(recs))
	for _, r := range recs {
		out.Items = append(out.Items, txToOut(r, cats, tags))
	}
	return nil, out, nil
}

type idInput struct {
	ID string `json:"id"`
}

func (m *mcpTools) getTransaction(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, txOut, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, txOut{}, err
	}
	r, err := m.owned(u.Id, "transactions", in.ID)
	if err != nil {
		return nil, txOut{}, err
	}
	return m.txResult(u.Id, r)
}

func (m *mcpTools) txResult(uid string, r *core.Record) (*mcp.CallToolResult, txOut, error) {
	cats, err := m.labels(uid, "categories")
	if err != nil {
		return nil, txOut{}, err
	}
	tags, err := m.labels(uid, "tags")
	if err != nil {
		return nil, txOut{}, err
	}
	return nil, txToOut(r, cats, tags), nil
}

type createTxInput struct {
	Type         string   `json:"type" jsonschema:"income or expense"`
	Amount       float64  `json:"amount" jsonschema:"gross amount in EUR, positive"`
	Date         string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD, defaults to today"`
	Area         string   `json:"area,omitempty" jsonschema:"business (default), rental or private"`
	Category     string   `json:"category,omitempty" jsonschema:"category name, created if new"`
	Tags         []string `json:"tags,omitempty" jsonschema:"tag names, created if new"`
	Note         string   `json:"note,omitempty"`
	LoanID       string   `json:"loan_id,omitempty" jsonschema:"record this expense as a payment on the given loan"`
	LoanInterest float64  `json:"loan_interest,omitempty" jsonschema:"interest part of a loan payment; it does not reduce the loan balance"`
}

func (m *mcpTools) createTransaction(ctx context.Context, _ *mcp.CallToolRequest, in createTxInput) (*mcp.CallToolResult, txOut, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, txOut{}, err
	}
	r, err := m.newTransaction(u.Id, in)
	if err != nil {
		return nil, txOut{}, err
	}
	return m.txResult(u.Id, r)
}

// normalized applies the input defaults (today, business) and validates the
// enum fields and the date.
func (in createTxInput) normalized() (createTxInput, time.Time, error) {
	if err := oneOf("type", in.Type, "income", "expense"); err != nil {
		return in, time.Time{}, err
	}
	if in.Area == "" {
		in.Area = "business"
	}
	if err := oneOf("area", in.Area, "business", "rental", "private"); err != nil {
		return in, time.Time{}, err
	}
	date := today()
	if in.Date != "" {
		var err error
		if date, err = parseDate(in.Date); err != nil {
			return in, time.Time{}, err
		}
	}
	return in, date, nil
}

// newTransaction validates one create input, resolves its labels and saves it.
func (m *mcpTools) newTransaction(uid string, in createTxInput) (*core.Record, error) {
	in, date, err := in.normalized()
	if err != nil {
		return nil, err
	}
	if in.LoanID != "" {
		if _, err := m.owned(uid, "loans", in.LoanID); err != nil {
			return nil, err
		}
	}
	cat, err := m.ensureLabel(uid, "categories", in.Category)
	if err != nil {
		return nil, err
	}
	tagIds, err := m.ensureTags(uid, in.Tags)
	if err != nil {
		return nil, err
	}
	col, err := m.app.FindCollectionByNameOrId("transactions")
	if err != nil {
		return nil, err
	}
	r := core.NewRecord(col)
	r.Set("user", uid)
	r.Set("type", in.Type)
	r.Set("date", date)
	r.Set("amount", in.Amount)
	r.Set("area", in.Area)
	r.Set("category", cat)
	r.Set("tags", tagIds)
	r.Set("note", in.Note)
	r.Set("loan", in.LoanID)
	r.Set("loan_interest", in.LoanInterest)
	if err := m.app.Save(r); err != nil {
		return nil, err
	}
	return r, nil
}

// findDuplicate returns the id of an existing transaction with the same
// date, type, amount (within a cent) and area, or "".
func (m *mcpTools) findDuplicate(uid string, in createTxInput) (string, error) {
	in, date, err := in.normalized()
	if err != nil {
		return "", err
	}
	recs, err := m.app.FindRecordsByFilter("transactions",
		"user = {:uid} && type = {:type} && area = {:area} && date >= {:from} && date < {:to} && amount > {:amin} && amount < {:amax}",
		"", 1, 0, dbx.Params{
			"uid": uid, "type": in.Type, "area": in.Area,
			"from": date.Format(types.DefaultDateLayout), "to": date.AddDate(0, 0, 1).Format(types.DefaultDateLayout),
			"amin": in.Amount - 0.005, "amax": in.Amount + 0.005,
		})
	if err != nil || len(recs) == 0 {
		return "", err
	}
	return recs[0].Id, nil
}

const maxBatchCreate = 200

type createTxBatchInput struct {
	Items          []createTxInput `json:"items" jsonschema:"transactions to create, in order (max 200)"`
	SkipDuplicates bool            `json:"skip_duplicates,omitempty" jsonschema:"skip items for which a transaction with the same date, type, amount and area already exists (they are listed in skipped); makes re-running an import safe"`
}

type skippedOut struct {
	Index       int    `json:"index" jsonschema:"position in items"`
	DuplicateOf string `json:"duplicate_of" jsonschema:"id of the existing transaction"`
}

type createTxBatchOutput struct {
	Items   []txOut      `json:"items" jsonschema:"the created transactions, in input order"`
	Created int          `json:"created"`
	Skipped []skippedOut `json:"skipped,omitempty"`
}

func (m *mcpTools) createTransactions(ctx context.Context, _ *mcp.CallToolRequest, in createTxBatchInput) (*mcp.CallToolResult, createTxBatchOutput, error) {
	out := createTxBatchOutput{Items: []txOut{}}
	u, err := writeUser(ctx)
	if err != nil {
		return nil, out, err
	}
	if len(in.Items) == 0 {
		return nil, out, errors.New("items must not be empty")
	}
	if len(in.Items) > maxBatchCreate {
		return nil, out, fmt.Errorf("at most %d items per call", maxBatchCreate)
	}
	var created []*core.Record
	err = m.app.RunInTransaction(func(txApp core.App) error {
		tx := &mcpTools{app: txApp}
		for i, item := range in.Items {
			if in.SkipDuplicates {
				dup, err := tx.findDuplicate(u.Id, item)
				if err != nil {
					return fmt.Errorf("item %d: %w", i, err)
				}
				if dup != "" {
					out.Skipped = append(out.Skipped, skippedOut{Index: i, DuplicateOf: dup})
					continue
				}
			}
			r, err := tx.newTransaction(u.Id, item)
			if err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
			created = append(created, r)
		}
		return nil
	})
	if err != nil {
		return nil, createTxBatchOutput{Items: []txOut{}}, err
	}
	cats, tags, err := m.catsAndTags(u.Id)
	if err != nil {
		return nil, out, err
	}
	for _, r := range created {
		out.Items = append(out.Items, txToOut(r, cats, tags))
	}
	out.Created = len(created)
	return nil, out, nil
}

type updateTxInput struct {
	ID           string   `json:"id"`
	Type         *string  `json:"type,omitempty" jsonschema:"income or expense"`
	Amount       *float64 `json:"amount,omitempty"`
	Date         *string  `json:"date,omitempty" jsonschema:"YYYY-MM-DD"`
	Area         *string  `json:"area,omitempty" jsonschema:"business, rental or private"`
	Category     *string  `json:"category,omitempty" jsonschema:"category name (created if new); 'none' clears it"`
	Tags         []string `json:"tags,omitempty" jsonschema:"replaces all tags; ['none'] clears them"`
	AddTags      []string `json:"add_tags,omitempty" jsonschema:"tag names to add (created if new), keeping the existing ones"`
	RemoveTags   []string `json:"remove_tags,omitempty" jsonschema:"tag names to remove"`
	Note         *string  `json:"note,omitempty"`
	LoanID       *string  `json:"loan_id,omitempty" jsonschema:"'none' unlinks the loan"`
	LoanInterest *float64 `json:"loan_interest,omitempty"`
}

func (m *mcpTools) updateTransaction(ctx context.Context, _ *mcp.CallToolRequest, in updateTxInput) (*mcp.CallToolResult, txOut, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, txOut{}, err
	}
	r, err := m.owned(u.Id, "transactions", in.ID)
	if err != nil {
		return nil, txOut{}, err
	}
	if in.Type != nil {
		if err := oneOf("type", *in.Type, "income", "expense"); err != nil {
			return nil, txOut{}, err
		}
		r.Set("type", *in.Type)
	}
	if in.Amount != nil {
		r.Set("amount", *in.Amount)
	}
	if in.Date != nil {
		d, err := parseDate(*in.Date)
		if err != nil {
			return nil, txOut{}, err
		}
		r.Set("date", d)
	}
	if in.Area != nil {
		if err := oneOf("area", *in.Area, "business", "rental", "private"); err != nil {
			return nil, txOut{}, err
		}
		r.Set("area", *in.Area)
	}
	if in.Category != nil {
		id, err := m.categoryID(u.Id, *in.Category)
		if err != nil {
			return nil, txOut{}, err
		}
		r.Set("category", id)
	}
	if in.Tags != nil {
		ids, err := m.ensureTags(u.Id, in.Tags)
		if err != nil {
			return nil, txOut{}, err
		}
		r.Set("tags", ids)
	}
	if len(in.AddTags) > 0 || len(in.RemoveTags) > 0 {
		ids, err := m.changeTags(u.Id, r.GetStringSlice("tags"), in.AddTags, in.RemoveTags)
		if err != nil {
			return nil, txOut{}, err
		}
		r.Set("tags", ids)
	}
	if in.Note != nil {
		r.Set("note", *in.Note)
	}
	if in.LoanID != nil {
		if cleared(*in.LoanID) {
			r.Set("loan", "")
		} else {
			if _, err := m.owned(u.Id, "loans", *in.LoanID); err != nil {
				return nil, txOut{}, err
			}
			r.Set("loan", strings.TrimSpace(*in.LoanID))
		}
	}
	if in.LoanInterest != nil {
		r.Set("loan_interest", *in.LoanInterest)
	}
	if err := m.app.Save(r); err != nil {
		return nil, txOut{}, err
	}
	return m.txResult(u.Id, r)
}

// categoryID resolves a category name for an update ("none"/"" → no category).
func (m *mcpTools) categoryID(uid, name string) (string, error) {
	if cleared(name) {
		return "", nil
	}
	return m.ensureLabel(uid, "categories", name)
}

// changeTags applies add/remove tag names to a list of tag ids. Added tags
// are created when unknown; removing an unknown tag is a no-op.
func (m *mcpTools) changeTags(uid string, current, add, remove []string) ([]string, error) {
	addIds, err := m.ensureTags(uid, add)
	if err != nil {
		return nil, err
	}
	drop := map[string]bool{}
	if len(remove) > 0 {
		all, err := m.labels(uid, "tags")
		if err != nil {
			return nil, err
		}
		for _, n := range remove {
			if id := findLabel(all, strings.TrimSpace(n)); id != "" {
				drop[id] = true
			}
		}
	}
	out := make([]string, 0, len(current)+len(addIds))
	seen := map[string]bool{}
	for _, id := range append(append([]string{}, current...), addIds...) {
		if !drop[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

const maxBatchTag = 500

type tagTxInput struct {
	IDs        []string `json:"ids" jsonschema:"transaction ids (max 500)"`
	AddTags    []string `json:"add_tags,omitempty" jsonschema:"tag names to add (created if new)"`
	RemoveTags []string `json:"remove_tags,omitempty" jsonschema:"tag names to remove"`
}

type updatedOutput struct {
	Updated int `json:"updated" jsonschema:"number of transactions changed"`
}

func (m *mcpTools) tagTransactions(ctx context.Context, _ *mcp.CallToolRequest, in tagTxInput) (*mcp.CallToolResult, updatedOutput, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, updatedOutput{}, err
	}
	if len(in.IDs) == 0 {
		return nil, updatedOutput{}, errors.New("ids must not be empty")
	}
	if len(in.IDs) > maxBatchTag {
		return nil, updatedOutput{}, fmt.Errorf("at most %d ids per call", maxBatchTag)
	}
	if len(in.AddTags) == 0 && len(in.RemoveTags) == 0 {
		return nil, updatedOutput{}, errors.New("nothing to do: give add_tags and/or remove_tags")
	}
	n := 0
	err = m.app.RunInTransaction(func(txApp core.App) error {
		tx := &mcpTools{app: txApp}
		for _, id := range in.IDs {
			r, err := tx.owned(u.Id, "transactions", id)
			if err != nil {
				return err
			}
			ids, err := tx.changeTags(u.Id, r.GetStringSlice("tags"), in.AddTags, in.RemoveTags)
			if err != nil {
				return err
			}
			r.Set("tags", ids)
			if err := txApp.Save(r); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return nil, updatedOutput{}, err
	}
	return nil, updatedOutput{Updated: n}, nil
}

type deletedOutput struct {
	Deleted string `json:"deleted" jsonschema:"id of the deleted record"`
}

func (m *mcpTools) deleteTransaction(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, deletedOutput, error) {
	return m.deleteOwned(ctx, "transactions", in.ID)
}

func (m *mcpTools) deleteOwned(ctx context.Context, col, id string) (*mcp.CallToolResult, deletedOutput, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, deletedOutput{}, err
	}
	r, err := m.owned(u.Id, col, id)
	if err != nil {
		return nil, deletedOutput{}, err
	}
	if err := m.app.Delete(r); err != nil {
		return nil, deletedOutput{}, err
	}
	return nil, deletedOutput{Deleted: r.Id}, nil
}

// --- categories & tags -------------------------------------------------------

type labelOut struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Transactions int    `json:"transactions" jsonschema:"number of transactions using it"`
}

type labelsOutput struct {
	Items []labelOut `json:"items"`
}

type emptyInput struct{}

func (m *mcpTools) listCategories(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, labelsOutput, error) {
	return m.listLabels(ctx, "categories", func(r *core.Record) []string { return []string{r.GetString("category")} })
}

func (m *mcpTools) listTags(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, labelsOutput, error) {
	return m.listLabels(ctx, "tags", func(r *core.Record) []string { return r.GetStringSlice("tags") })
}

func (m *mcpTools) listLabels(ctx context.Context, col string, idsOf func(*core.Record) []string) (*mcp.CallToolResult, labelsOutput, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, labelsOutput{}, err
	}
	labels, err := m.labels(u.Id, col)
	if err != nil {
		return nil, labelsOutput{}, err
	}
	txs, err := m.app.FindRecordsByFilter("transactions", "user = {:uid}", "", 0, 0, dbx.Params{"uid": u.Id})
	if err != nil {
		return nil, labelsOutput{}, err
	}
	counts := map[string]int{}
	for _, t := range txs {
		for _, id := range idsOf(t) {
			counts[id]++
		}
	}
	out := labelsOutput{Items: make([]labelOut, 0, len(labels))}
	for id, name := range labels {
		out.Items = append(out.Items, labelOut{ID: id, Name: name, Transactions: counts[id]})
	}
	sort.Slice(out.Items, func(i, j int) bool { return strings.ToLower(out.Items[i].Name) < strings.ToLower(out.Items[j].Name) })
	return nil, out, nil
}

// labelCol maps the tool-facing kind to its collection.
func labelCol(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "category", "categories":
		return "categories", nil
	case "tag", "tags":
		return "tags", nil
	}
	return "", errors.New("kind must be category or tag")
}

// labelUsers loads the user's transactions and recurring templates that use
// a label (category relation or tags list), so a merge can relink them.
func (m *mcpTools) labelUsers(uid, col, id string) ([]*core.Record, error) {
	field := "category"
	op := "="
	if col == "tags" {
		field, op = "tags", "~"
	}
	var out []*core.Record
	for _, c := range []string{"transactions", "recurring"} {
		recs, err := m.app.FindRecordsByFilter(c, "user = {:uid} && "+field+" "+op+" {:id}", "", 0, 0, dbx.Params{"uid": uid, "id": id})
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

type renameLabelInput struct {
	Kind    string `json:"kind" jsonschema:"category or tag"`
	Name    string `json:"name" jsonschema:"current name"`
	NewName string `json:"new_name" jsonschema:"new name; if it already exists the two are merged"`
}

type renameLabelOutput struct {
	labelOut
	Merged bool `json:"merged" jsonschema:"true when the label was merged into an existing one"`
}

func (m *mcpTools) renameLabel(ctx context.Context, _ *mcp.CallToolRequest, in renameLabelInput) (*mcp.CallToolResult, renameLabelOutput, error) {
	var out renameLabelOutput
	u, err := writeUser(ctx)
	if err != nil {
		return nil, out, err
	}
	col, err := labelCol(in.Kind)
	if err != nil {
		return nil, out, err
	}
	newName := strings.TrimSpace(in.NewName)
	if newName == "" {
		return nil, out, errors.New("new_name must not be empty")
	}
	all, err := m.labels(u.Id, col)
	if err != nil {
		return nil, out, err
	}
	oldID := findLabel(all, strings.TrimSpace(in.Name))
	if oldID == "" {
		return nil, out, fmt.Errorf("%s %q not found", strings.TrimSuffix(col, "s"), in.Name)
	}
	targetID := findLabel(all, newName)
	if targetID == "" || targetID == oldID {
		// plain rename (also case-only changes)
		r, err := m.app.FindRecordById(col, oldID)
		if err != nil {
			return nil, out, err
		}
		r.Set("name", newName)
		if err := m.app.Save(r); err != nil {
			return nil, out, err
		}
		targetID = oldID
	} else {
		// merge: relink every user of the old label, then delete it
		users, err := m.labelUsers(u.Id, col, oldID)
		if err != nil {
			return nil, out, err
		}
		err = m.app.RunInTransaction(func(txApp core.App) error {
			for _, r := range users {
				if col == "categories" {
					r.Set("category", targetID)
				} else {
					ids := []string{}
					for _, id := range r.GetStringSlice("tags") {
						if id != oldID && id != targetID {
							ids = append(ids, id)
						}
					}
					r.Set("tags", append(ids, targetID))
				}
				if err := txApp.Save(r); err != nil {
					return err
				}
			}
			old, err := txApp.FindRecordById(col, oldID)
			if err != nil {
				return err
			}
			return txApp.Delete(old)
		})
		if err != nil {
			return nil, out, err
		}
		out.Merged = true
	}
	n, err := m.app.CountRecords("transactions", dbx.NewExp(map[string]string{"categories": "category = {:id}", "tags": "tags LIKE {:like}"}[col],
		dbx.Params{"id": targetID, "like": "%" + targetID + "%"}))
	if err != nil {
		return nil, out, err
	}
	out.labelOut = labelOut{ID: targetID, Name: all[targetID], Transactions: int(n)}
	if targetID == oldID {
		out.Name = newName
	}
	return nil, out, nil
}

type deleteLabelInput struct {
	Kind string `json:"kind" jsonschema:"category or tag"`
	Name string `json:"name"`
}

type deleteLabelOutput struct {
	Deleted      string `json:"deleted" jsonschema:"id of the deleted label"`
	Transactions int    `json:"transactions" jsonschema:"how many transactions were using it (they are kept)"`
}

func (m *mcpTools) deleteLabel(ctx context.Context, _ *mcp.CallToolRequest, in deleteLabelInput) (*mcp.CallToolResult, deleteLabelOutput, error) {
	var out deleteLabelOutput
	u, err := writeUser(ctx)
	if err != nil {
		return nil, out, err
	}
	col, err := labelCol(in.Kind)
	if err != nil {
		return nil, out, err
	}
	all, err := m.labels(u.Id, col)
	if err != nil {
		return nil, out, err
	}
	id := findLabel(all, strings.TrimSpace(in.Name))
	if id == "" {
		return nil, out, fmt.Errorf("%s %q not found", strings.TrimSuffix(col, "s"), in.Name)
	}
	users, err := m.labelUsers(u.Id, col, id)
	if err != nil {
		return nil, out, err
	}
	for _, r := range users {
		if r.Collection().Name == "transactions" {
			out.Transactions++
		}
	}
	r, err := m.app.FindRecordById(col, id)
	if err != nil {
		return nil, out, err
	}
	if err := m.app.Delete(r); err != nil { // relations are cleared by PocketBase
		return nil, out, err
	}
	out.Deleted = id
	return nil, out, nil
}

// --- year summary --------------------------------------------------------------

type bucket struct {
	Income   float64 `json:"income"`
	Expenses float64 `json:"expenses"`
	Net      float64 `json:"net"`
}

func (b *bucket) add(typ string, amount float64) {
	if typ == "income" {
		b.Income += amount
	} else {
		b.Expenses += amount
	}
	b.Net = b.Income - b.Expenses
}

type projection struct {
	Income   float64            `json:"income"`
	Expenses float64            `json:"expenses"`
	Net      float64            `json:"net"`
	Area     map[string]*bucket `json:"area"`
}

type summaryInput struct {
	Year int `json:"year,omitempty" jsonschema:"calendar year, defaults to the current year"`
}

type summaryOutput struct {
	Year         int                `json:"year"`
	Transactions int                `json:"transactions" jsonschema:"number of transactions in the year"`
	Income       float64            `json:"income"`
	Expenses     float64            `json:"expenses"`
	Net          float64            `json:"net"`
	Area         map[string]*bucket `json:"area" jsonschema:"business, rental, private"`
	Category     map[string]*bucket `json:"category" jsonschema:"by category name; '(none)' for uncategorized"`
	Tag          map[string]*bucket `json:"tag" jsonschema:"by tag name"`
	Projected    *projection        `json:"projected,omitempty" jsonschema:"recurring amounts still planned for the rest of the year (not yet in the totals)"`
}

var areas = []string{"business", "rental", "private"}

func newAreaBuckets() map[string]*bucket {
	m := make(map[string]*bucket, len(areas))
	for _, a := range areas {
		m[a] = &bucket{}
	}
	return m
}

func (m *mcpTools) yearSummary(ctx context.Context, _ *mcp.CallToolRequest, in summaryInput) (*mcp.CallToolResult, summaryOutput, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, summaryOutput{}, err
	}
	year := in.Year
	if year <= 0 {
		year = today().Year()
	}
	cats, err := m.labels(u.Id, "categories")
	if err != nil {
		return nil, summaryOutput{}, err
	}
	tags, err := m.labels(u.Id, "tags")
	if err != nil {
		return nil, summaryOutput{}, err
	}
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	txs, err := m.app.FindRecordsByFilter("transactions", "user = {:uid} && date >= {:from} && date < {:to}", "date", 0, 0,
		dbx.Params{"uid": u.Id, "from": from.Format(types.DefaultDateLayout), "to": from.AddDate(1, 0, 0).Format(types.DefaultDateLayout)})
	if err != nil {
		return nil, summaryOutput{}, err
	}

	out := summaryOutput{Year: year, Transactions: len(txs), Area: newAreaBuckets(), Category: map[string]*bucket{}, Tag: map[string]*bucket{}}
	total := &bucket{}
	for _, t := range txs {
		typ, amount := t.GetString("type"), t.GetFloat("amount")
		total.add(typ, amount)
		if b := out.Area[t.GetString("area")]; b != nil {
			b.add(typ, amount)
		}
		cat := cats[t.GetString("category")]
		if cat == "" {
			cat = "(none)"
		}
		if out.Category[cat] == nil {
			out.Category[cat] = &bucket{}
		}
		out.Category[cat].add(typ, amount)
		for _, id := range t.GetStringSlice("tags") {
			name, ok := tags[id]
			if !ok {
				continue
			}
			if out.Tag[name] == nil {
				out.Tag[name] = &bucket{}
			}
			out.Tag[name].add(typ, amount)
		}
	}
	out.Income, out.Expenses, out.Net = total.Income, total.Expenses, total.Net

	templates, err := m.app.FindRecordsByFilter("recurring", "user = {:uid} && active = true", "", 0, 0, dbx.Params{"uid": u.Id})
	if err != nil {
		return nil, summaryOutput{}, err
	}
	if p := projectRecurring(templates, year, today()); p.Income != 0 || p.Expenses != 0 {
		out.Projected = &p
	}
	return nil, out, nil
}

// occurrences returns the occurrence dates of a template in (afterExcl, toIncl],
// honoring its end date. Mirrors occurrences() in ui/src/lib/recurring.ts.
func occurrences(rec *core.Record, afterExcl, toIncl time.Time) []time.Time {
	start := rec.GetDateTime("start").Time().UTC().Truncate(24 * time.Hour)
	if start.IsZero() {
		return nil
	}
	limit := toIncl
	if end := rec.GetDateTime("end").Time().UTC(); !end.IsZero() && end.Before(limit) {
		limit = end
	}
	interval, weekdaysOnly := rec.GetString("interval"), rec.GetBool("weekdays_only")
	var out []time.Time
	for n := 0; n < maxPerRun; n++ {
		d := nthOccurrence(start, interval, n)
		if weekdaysOnly {
			d = shiftToWeekday(d)
		}
		if d.After(limit) {
			break
		}
		if d.After(afterExcl) {
			out = append(out, d)
		}
	}
	return out
}

// projectRecurring sums the not-yet-materialized occurrences of a year. The
// generator only creates occurrences <= today and we only project after
// max(today, last_generated), so actual + projected never double-counts.
func projectRecurring(templates []*core.Record, year int, today time.Time) projection {
	p := projection{Area: newAreaBuckets()}
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	total := &bucket{}
	for _, t := range templates {
		if !t.GetBool("active") {
			continue
		}
		after := today
		if wm := t.GetDateTime("last_generated").Time().UTC(); wm.After(after) {
			after = wm
		}
		for _, d := range occurrences(t, after, yearEnd) {
			if d.Before(yearStart) {
				continue
			}
			total.add(t.GetString("type"), t.GetFloat("amount"))
			if b := p.Area[t.GetString("area")]; b != nil {
				b.add(t.GetString("type"), t.GetFloat("amount"))
			}
		}
	}
	p.Income, p.Expenses, p.Net = total.Income, total.Expenses, total.Net
	return p
}

type taxRuleOutput struct {
	Name   string `json:"name"`
	Script string `json:"script" jsonschema:"JavaScript function body; receives d, returns [{label, value, hint?}]"`
}

func (m *mcpTools) getTaxRule(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, taxRuleOutput, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, taxRuleOutput{}, err
	}
	r, err := m.app.FindFirstRecordByFilter("rules", "user = {:uid} && active = true", dbx.Params{"uid": u.Id})
	if err != nil {
		return nil, taxRuleOutput{}, errors.New("no active tax rule")
	}
	return nil, taxRuleOutput{Name: r.GetString("name"), Script: r.GetString("script")}, nil
}

// --- recurring ---------------------------------------------------------------

// validInterval returns the canonical form of an interval or a tool error.
func validInterval(s string) (string, error) {
	canon, ok := canonicalInterval(s)
	if !ok {
		return "", fmt.Errorf("interval must be weekly, monthly, quarterly, half-yearly, yearly or '<n> weeks|months|years', got %q", s)
	}
	return canon, nil
}

type recurringOut struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Amount        float64  `json:"amount"`
	Area          string   `json:"area"`
	Category      string   `json:"category,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Note          string   `json:"note,omitempty"`
	Interval      string   `json:"interval" jsonschema:"weekly, monthly, quarterly, half-yearly, yearly, or '<n> weeks|months|years' (e.g. '2 weeks', '18 months')"`
	Start         string   `json:"start"`
	End           string   `json:"end,omitempty"`
	WeekdaysOnly  bool     `json:"weekdays_only" jsonschema:"Saturday/Sunday occurrences move to the following Monday"`
	Active        bool     `json:"active"`
	LastGenerated string   `json:"last_generated,omitempty" jsonschema:"newest occurrence already turned into a transaction"`
	Next          string   `json:"next,omitempty" jsonschema:"next occurrence after today; empty when paused or ended"`
}

func recurringToOut(r *core.Record, cats, tags map[string]string) recurringOut {
	o := recurringOut{
		ID:            r.Id,
		Type:          r.GetString("type"),
		Amount:        r.GetFloat("amount"),
		Area:          r.GetString("area"),
		Category:      cats[r.GetString("category")],
		Note:          r.GetString("note"),
		Interval:      r.GetString("interval"),
		Start:         dateOut(r.GetDateTime("start")),
		End:           dateOut(r.GetDateTime("end")),
		WeekdaysOnly:  r.GetBool("weekdays_only"),
		Active:        r.GetBool("active"),
		LastGenerated: dateOut(r.GetDateTime("last_generated")),
	}
	for _, id := range r.GetStringSlice("tags") {
		if n, ok := tags[id]; ok {
			o.Tags = append(o.Tags, n)
		}
	}
	if o.Active {
		// The next occurrence lies within one step after max(today, start)
		// (+ at most 2 days of weekday shift); two steps is a safe horizon.
		base := today()
		if start := r.GetDateTime("start").Time().UTC(); start.After(base) {
			base = start
		}
		if next := occurrences(r, today(), nthOccurrence(base, o.Interval, 2)); len(next) > 0 {
			o.Next = next[0].Format(dateLayout)
		}
	}
	return o
}

type recurringListOutput struct {
	Items []recurringOut `json:"items"`
}

func (m *mcpTools) listRecurring(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, recurringListOutput, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, recurringListOutput{}, err
	}
	cats, tags, err := m.catsAndTags(u.Id)
	if err != nil {
		return nil, recurringListOutput{}, err
	}
	recs, err := m.app.FindRecordsByFilter("recurring", "user = {:uid}", "-active,start", 0, 0, dbx.Params{"uid": u.Id})
	if err != nil {
		return nil, recurringListOutput{}, err
	}
	out := recurringListOutput{Items: make([]recurringOut, 0, len(recs))}
	for _, r := range recs {
		out.Items = append(out.Items, recurringToOut(r, cats, tags))
	}
	return nil, out, nil
}

func (m *mcpTools) catsAndTags(uid string) (map[string]string, map[string]string, error) {
	cats, err := m.labels(uid, "categories")
	if err != nil {
		return nil, nil, err
	}
	tags, err := m.labels(uid, "tags")
	if err != nil {
		return nil, nil, err
	}
	return cats, tags, nil
}

type createRecurringInput struct {
	Type         string   `json:"type" jsonschema:"income or expense"`
	Amount       float64  `json:"amount" jsonschema:"amount per occurrence in EUR"`
	Interval     string   `json:"interval" jsonschema:"weekly, monthly, quarterly, half-yearly, yearly, or '<n> weeks|months|years' (e.g. '2 weeks', '18 months')"`
	Start        string   `json:"start,omitempty" jsonschema:"first occurrence, YYYY-MM-DD, defaults to today; 'every 1st' = a start on the 1st"`
	End          string   `json:"end,omitempty" jsonschema:"last possible date, YYYY-MM-DD; empty = open-ended"`
	Area         string   `json:"area,omitempty" jsonschema:"business (default), rental or private"`
	Category     string   `json:"category,omitempty" jsonschema:"category name, created if new"`
	Tags         []string `json:"tags,omitempty" jsonschema:"tag names, created if new"`
	Note         string   `json:"note,omitempty"`
	WeekdaysOnly bool     `json:"weekdays_only,omitempty" jsonschema:"move Saturday/Sunday occurrences to the following Monday"`
}

type createRecurringOutput struct {
	recurringOut
	Generated int `json:"generated" jsonschema:"past occurrences that were created as transactions right away"`
}

func (m *mcpTools) createRecurring(ctx context.Context, _ *mcp.CallToolRequest, in createRecurringInput) (*mcp.CallToolResult, createRecurringOutput, error) {
	var out createRecurringOutput
	u, err := writeUser(ctx)
	if err != nil {
		return nil, out, err
	}
	if err := oneOf("type", in.Type, "income", "expense"); err != nil {
		return nil, out, err
	}
	interval, err := validInterval(in.Interval)
	if err != nil {
		return nil, out, err
	}
	if in.Area == "" {
		in.Area = "business"
	}
	if err := oneOf("area", in.Area, "business", "rental", "private"); err != nil {
		return nil, out, err
	}
	start := today()
	if in.Start != "" {
		if start, err = parseDate(in.Start); err != nil {
			return nil, out, err
		}
	}
	var end any
	if in.End != "" {
		e, err := parseDate(in.End)
		if err != nil {
			return nil, out, err
		}
		if e.Before(start) {
			return nil, out, errors.New("end must not be before start")
		}
		end = e
	}
	cat, err := m.ensureLabel(u.Id, "categories", in.Category)
	if err != nil {
		return nil, out, err
	}
	tagIds, err := m.ensureTags(u.Id, in.Tags)
	if err != nil {
		return nil, out, err
	}
	col, err := m.app.FindCollectionByNameOrId("recurring")
	if err != nil {
		return nil, out, err
	}
	r := core.NewRecord(col)
	r.Set("user", u.Id)
	r.Set("type", in.Type)
	r.Set("amount", in.Amount)
	r.Set("area", in.Area)
	r.Set("category", cat)
	r.Set("tags", tagIds)
	r.Set("note", in.Note)
	r.Set("interval", interval)
	r.Set("start", start)
	r.Set("end", end)
	r.Set("weekdays_only", in.WeekdaysOnly)
	r.Set("active", true)
	if err := m.app.Save(r); err != nil { // the create hook backfills past occurrences
		return nil, out, err
	}
	n, err := m.app.CountRecords("transactions", dbx.HashExp{"recurring": r.Id})
	if err != nil {
		return nil, out, err
	}
	if r, err = m.app.FindRecordById("recurring", r.Id); err != nil { // reload last_generated
		return nil, out, err
	}
	cats, tags, err := m.catsAndTags(u.Id)
	if err != nil {
		return nil, out, err
	}
	return nil, createRecurringOutput{recurringOut: recurringToOut(r, cats, tags), Generated: int(n)}, nil
}

type updateRecurringInput struct {
	ID           string   `json:"id"`
	Type         *string  `json:"type,omitempty" jsonschema:"income or expense"`
	Amount       *float64 `json:"amount,omitempty"`
	Interval     *string  `json:"interval,omitempty" jsonschema:"weekly, monthly, quarterly, half-yearly, yearly, or '<n> weeks|months|years' (e.g. '2 weeks', '18 months')"`
	Start        *string  `json:"start,omitempty" jsonschema:"YYYY-MM-DD"`
	End          *string  `json:"end,omitempty" jsonschema:"YYYY-MM-DD; 'none' makes it open-ended"`
	Area         *string  `json:"area,omitempty" jsonschema:"business, rental or private"`
	Category     *string  `json:"category,omitempty" jsonschema:"category name (created if new); 'none' clears it"`
	Tags         []string `json:"tags,omitempty" jsonschema:"replaces all tags; ['none'] clears them"`
	Note         *string  `json:"note,omitempty"`
	WeekdaysOnly *bool    `json:"weekdays_only,omitempty"`
	Active       *bool    `json:"active,omitempty" jsonschema:"false pauses the template"`
}

func (m *mcpTools) updateRecurring(ctx context.Context, _ *mcp.CallToolRequest, in updateRecurringInput) (*mcp.CallToolResult, recurringOut, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, recurringOut{}, err
	}
	r, err := m.owned(u.Id, "recurring", in.ID)
	if err != nil {
		return nil, recurringOut{}, err
	}
	if in.Type != nil {
		if err := oneOf("type", *in.Type, "income", "expense"); err != nil {
			return nil, recurringOut{}, err
		}
		r.Set("type", *in.Type)
	}
	if in.Amount != nil {
		r.Set("amount", *in.Amount)
	}
	if in.Interval != nil {
		interval, err := validInterval(*in.Interval)
		if err != nil {
			return nil, recurringOut{}, err
		}
		r.Set("interval", interval)
	}
	if in.Start != nil {
		d, err := parseDate(*in.Start)
		if err != nil {
			return nil, recurringOut{}, err
		}
		r.Set("start", d)
	}
	if in.End != nil {
		if cleared(*in.End) {
			r.Set("end", nil)
		} else {
			d, err := parseDate(*in.End)
			if err != nil {
				return nil, recurringOut{}, err
			}
			r.Set("end", d)
		}
	}
	if in.Area != nil {
		if err := oneOf("area", *in.Area, "business", "rental", "private"); err != nil {
			return nil, recurringOut{}, err
		}
		r.Set("area", *in.Area)
	}
	if in.Category != nil {
		id, err := m.categoryID(u.Id, *in.Category)
		if err != nil {
			return nil, recurringOut{}, err
		}
		r.Set("category", id)
	}
	if in.Tags != nil {
		ids, err := m.ensureTags(u.Id, in.Tags)
		if err != nil {
			return nil, recurringOut{}, err
		}
		r.Set("tags", ids)
	}
	if in.Note != nil {
		r.Set("note", *in.Note)
	}
	if in.WeekdaysOnly != nil {
		r.Set("weekdays_only", *in.WeekdaysOnly)
	}
	if in.Active != nil {
		r.Set("active", *in.Active)
	}
	if err := m.app.Save(r); err != nil {
		return nil, recurringOut{}, err
	}
	if r, err = m.app.FindRecordById("recurring", r.Id); err != nil {
		return nil, recurringOut{}, err
	}
	cats, tags, err := m.catsAndTags(u.Id)
	if err != nil {
		return nil, recurringOut{}, err
	}
	return nil, recurringToOut(r, cats, tags), nil
}

func (m *mcpTools) deleteRecurring(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, deletedOutput, error) {
	return m.deleteOwned(ctx, "recurring", in.ID)
}

// --- loans ---------------------------------------------------------------------

type loanOut struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Principal    float64 `json:"principal"`
	InterestRate float64 `json:"interest_rate" jsonschema:"annual %, informational"`
	Start        string  `json:"start,omitempty"`
	Note         string  `json:"note,omitempty"`
	Closed       bool    `json:"closed"`
	Payments     int     `json:"payments" jsonschema:"number of payment transactions"`
	Repaid       float64 `json:"repaid" jsonschema:"sum of payments excluding interest"`
	InterestPaid float64 `json:"interest_paid"`
	Remaining    float64 `json:"remaining" jsonschema:"principal - repaid"`
}

type loansOutput struct {
	Items []loanOut `json:"items"`
}

func (m *mcpTools) listLoans(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, loansOutput, error) {
	u, err := authUser(ctx)
	if err != nil {
		return nil, loansOutput{}, err
	}
	loans, err := m.app.FindRecordsByFilter("loans", "user = {:uid}", "closed,name", 0, 0, dbx.Params{"uid": u.Id})
	if err != nil {
		return nil, loansOutput{}, err
	}
	out := loansOutput{Items: make([]loanOut, 0, len(loans))}
	for _, l := range loans {
		o, err := m.loanOut(l)
		if err != nil {
			return nil, loansOutput{}, err
		}
		out.Items = append(out.Items, o)
	}
	return nil, out, nil
}

func (m *mcpTools) loanOut(l *core.Record) (loanOut, error) {
	o := loanOut{
		ID:           l.Id,
		Name:         l.GetString("name"),
		Principal:    l.GetFloat("principal"),
		InterestRate: l.GetFloat("interest_rate"),
		Start:        dateOut(l.GetDateTime("start")),
		Note:         l.GetString("note"),
		Closed:       l.GetBool("closed"),
	}
	payments, err := m.app.FindRecordsByFilter("transactions", "loan = {:id}", "", 0, 0, dbx.Params{"id": l.Id})
	if err != nil {
		return o, err
	}
	for _, p := range payments {
		interest := p.GetFloat("loan_interest")
		o.Payments++
		o.InterestPaid += interest
		o.Repaid += p.GetFloat("amount") - interest
	}
	o.Remaining = o.Principal - o.Repaid
	return o, nil
}

type createLoanInput struct {
	Name         string  `json:"name"`
	Principal    float64 `json:"principal" jsonschema:"amount borrowed in EUR"`
	InterestRate float64 `json:"interest_rate,omitempty" jsonschema:"annual %, 0 = interest-free"`
	Start        string  `json:"start,omitempty" jsonschema:"YYYY-MM-DD"`
	Note         string  `json:"note,omitempty"`
}

func (m *mcpTools) createLoan(ctx context.Context, _ *mcp.CallToolRequest, in createLoanInput) (*mcp.CallToolResult, loanOut, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, loanOut{}, err
	}
	var start any
	if in.Start != "" {
		if start, err = parseDate(in.Start); err != nil {
			return nil, loanOut{}, err
		}
	}
	col, err := m.app.FindCollectionByNameOrId("loans")
	if err != nil {
		return nil, loanOut{}, err
	}
	r := core.NewRecord(col)
	r.Set("user", u.Id)
	r.Set("name", strings.TrimSpace(in.Name))
	r.Set("principal", in.Principal)
	r.Set("interest_rate", in.InterestRate)
	r.Set("start", start)
	r.Set("note", in.Note)
	if err := m.app.Save(r); err != nil {
		return nil, loanOut{}, err
	}
	o, err := m.loanOut(r)
	return nil, o, err
}

type updateLoanInput struct {
	ID           string   `json:"id"`
	Name         *string  `json:"name,omitempty"`
	Principal    *float64 `json:"principal,omitempty" jsonschema:"amount borrowed in EUR"`
	InterestRate *float64 `json:"interest_rate,omitempty" jsonschema:"annual %"`
	Start        *string  `json:"start,omitempty" jsonschema:"YYYY-MM-DD; 'none' clears it"`
	Note         *string  `json:"note,omitempty"`
	Closed       *bool    `json:"closed,omitempty" jsonschema:"true archives the loan (hidden from the overview), false reopens it"`
}

func (m *mcpTools) updateLoan(ctx context.Context, _ *mcp.CallToolRequest, in updateLoanInput) (*mcp.CallToolResult, loanOut, error) {
	u, err := writeUser(ctx)
	if err != nil {
		return nil, loanOut{}, err
	}
	r, err := m.owned(u.Id, "loans", in.ID)
	if err != nil {
		return nil, loanOut{}, err
	}
	if in.Name != nil {
		r.Set("name", strings.TrimSpace(*in.Name))
	}
	if in.Principal != nil {
		r.Set("principal", *in.Principal)
	}
	if in.InterestRate != nil {
		r.Set("interest_rate", *in.InterestRate)
	}
	if in.Start != nil {
		if cleared(*in.Start) {
			r.Set("start", nil)
		} else {
			d, err := parseDate(*in.Start)
			if err != nil {
				return nil, loanOut{}, err
			}
			r.Set("start", d)
		}
	}
	if in.Note != nil {
		r.Set("note", *in.Note)
	}
	if in.Closed != nil {
		r.Set("closed", *in.Closed)
	}
	if err := m.app.Save(r); err != nil {
		return nil, loanOut{}, err
	}
	o, err := m.loanOut(r)
	return nil, o, err
}

func (m *mcpTools) deleteLoan(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, deletedOutput, error) {
	return m.deleteOwned(ctx, "loans", in.ID)
}

func ptr[T any](v T) *T { return &v }

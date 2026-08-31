# murmelmoney — minimal self-hosted personal finance tracker

Spec / implementation plan. Scope is deliberately tiny: track incomes and expenses,
attach files and notes, and get a *rough* yearly tax estimate. Nothing else.

## 1. Goals & non-goals

**Goals**
- Add / edit / delete expenses and incomes
- Attach files (receipts, invoices, PDFs, images) and a free-text note to each transaction
- Distinguish *business* (freelance SE), *rental* (house) and *private* money
- Yearly overview: totals per type / area / category
- Rough tax estimate per year, driven by a small user-editable script (not hard-coded tax law)
- Multi-user (register/login, strict per-user data), self-hosted, single binary, one Docker image, one data volume
- Per-user creatable **categories** (one per transaction) and **tags** (many; clients, projects, cost points)

**Non-goals** (explicitly out of scope, at least for v1)
- Bank sync / imports, budgeting/envelopes, multi-currency, multi-user, invoicing,
  double-entry accounting, VAT/USt bookkeeping, mobile app, exact tax filing numbers.

## 2. Stack

| Layer | Choice | Notes |
|---|---|---|
| Backend | Go + PocketBase (framework mode, v0.39+) | gives SQLite, REST API, auth, file storage, admin UI for free |
| Frontend | Svelte 5 + Vite, plain CSS, `pocketbase` JS SDK | hash-based routing (`#/`, `#/transactions`, `#/rules`), no router lib |
| Packaging | `//go:embed ui/dist` → served via `apis.Static(..., true)` | one static binary |
| Docker | multi-stage: `node:alpine` (vite build) → `golang:alpine` (CGO off) → `scratch`/`alpine` | data volume at `/pb_data` |
| Auth | PocketBase `users` collection | register + login in UI; every record has a `user` relation, API rules enforce `user = @request.auth.id`; registration open until the first user exists, `MURMEL_REGISTRATION=true/false` forces it; `GET /api/murmel/status` tells the login page |

## 3. Repository layout

```
finance/
├── main.go               # pocketbase app: embed ui, register migrations, bootstrap superuser, serve
├── migrations/
│   └── 0001_init.go      # creates collections + default rule record
├── assets/logo/          # logo (svg/png), -sym variant for hero images
├── ui/                   # Svelte app
│   ├── src/
│   │   ├── App.svelte    # shell: login gate + hash router + nav
│   │   ├── lib/pb.ts     # PocketBase client singleton (base URL = window.location.origin)
│   │   ├── lib/tax.ts    # aggregate() + runRule()
│   │   ├── lib/format.ts # currency / date helpers
│   │   └── pages/
│   │       ├── Overview.svelte
│   │       ├── Transactions.svelte
│   │       ├── TransactionForm.svelte
│   │       └── Rules.svelte
│   ├── package.json, vite.config.ts, svelte.config.js
├── Dockerfile
├── docker-compose.yml
├── Makefile              # dev / build / docker targets
└── README.md
```

## 4. Data model (PocketBase collections)

### `categories` / `tags` (base) — `user` (relation, cascade), `name` (unique per user)

### `transactions` (base)
| field | type | notes |
|---|---|---|
| `user` | relation → users | required, cascade delete |
| `type` | select `income` \| `expense` | required |
| `date` | date | required, defaults to today |
| `amount` | number, min 0 | required, gross amount in EUR (positive; sign implied by `type`) |
| `area` | select `business` \| `rental` \| `private` | required, default `business` |
| `category` | relation → categories (single) | created on the fly from the form |
| `tags` | relation → tags (multi) | created on the fly (chip picker) |
| `note` | text | free-form, multiline |
| `attachments` | file, multiple (max 10, 25 MB each), **protected** | served only with a file token of the owner (`pb.files.getToken()`) |
| `created`, `updated` | autodate | |

API rules: owner-only (`@request.auth.id != '' && user = @request.auth.id`; create requires `@request.body.user = @request.auth.id`). Index on `(user, date)`.

### `recurring` (base) — templates for auto-generated transactions
| field | type | notes |
|---|---|---|
| `user` | relation → users | required, cascade delete |
| `type` / `amount` / `area` / `category` / `tags` / `note` | | same shapes as on `transactions`; copied into each generated transaction |
| `interval` | select `weekly` \| `monthly` \| `quarterly` \| `yearly` | required |
| `start` | date | required; occurrence *n* is always `start + n·interval` (day-of-month clamped to shorter months, weekly keeps the weekday) — so "every 1st" / "every Friday" is just the start date |
| `end` | date | optional; generation and projection stop there |
| `weekdays_only` | bool | shift Sat/Sun occurrences to the following Monday — "first weekday of the month" = start on the 1st + this (v5) |
| `active` | bool | paused templates generate nothing and are not projected |
| `last_generated` | date | server-maintained watermark (newest generated occurrence) |

Owner-only rules, index on `(user, active)`. See §7 for the generator.

### `loans` (base) — not bound to a year
| field | type | notes |
|---|---|---|
| `user` | relation → users | required, cascade delete |
| `name` | text | required |
| `principal` | number, min 0 | required |
| `interest_rate` | number, min 0 | annual %, informational (0 = interest-free) |
| `start` / `note` / `attachments` / `closed` | | attachments protected like on transactions; `closed` archives the loan |

Owner-only rules. Payments are normal expense `transactions` with two extra fields added in v4:
`loan` (relation → loans) and `loan_interest` (number — the interest part of the payment, which
does **not** reduce the balance). `transactions` also gained `recurring` (relation → recurring,
provenance of generated rows). All three relations keep the transaction when the target is
deleted (PocketBase clears the id). Remaining balance = `principal − Σ(amount − loan_interest)`,
computed client-side (partial index on `loan` where `loan != ''`).

### `rules` (base)
| field | type | notes |
|---|---|---|
| `user` | relation → users | required, cascade delete; a default rule is inserted for every new user (Go hook) |
| `name` | text | e.g. "AT rough estimate 2025" |
| `script` | text | JavaScript body, see §6 |
| `active` | bool | the rule used on the overview page (exactly one, enforced in UI) |

### `settings` — not needed for v1 (currency fixed to EUR, year picker derived from data).

## 5. Pages / UX

**Login / Register** — `pb.collection('users')` create + `authWithPassword()`. Token kept in localStorage by the SDK.

**Transactions (`#/transactions`)**
- Filters: year (default current), type, area, text search (category/note)
- Table: date · type · area · category · amount · 📎 count · note preview; click row → edit
- Row click → read-only **detail view** (amount, date, area, category, tags, full note, attachment grid with image previews; Edit / Delete / Close)
- "New income" / "New expense" buttons open the form; Edit in the detail view opens the same form
- Form: type, date, amount, area, category (datalist), note (textarea), file dropzone (multi), existing attachments with open/delete
- New transactions can be saved as recurring right there: a "Repeats" select (weekly…yearly, plus until/weekdays-only) turns the save into a recurring template starting at the chosen date; the loan dropdown has a "+ new loan…" entry opening the loan form inline
- Delete with confirm

**Overview (`#/`)**
- Year picker
- Tiles: total income, total expenses, net; per area (business / rental / private) income / expenses / net
- Projection tiles (only when something is still planned): actual + upcoming recurring occurrences of the year
- Loans panel (only when open loans exist): repaid / interest / remaining / progress per loan + total debt — current state, deliberately separate from the yearly aggregates
- Table: net by category
- **Tax estimate** panel: runs the active rule against the year's aggregate and shows the returned lines (label · value). Errors in the script are shown inline, never crash the page.

**Recurring (`#/recurring`)** — template list (interval, next occurrence, paused/ended state) with a
form like the transaction form minus attachments plus interval/start/end/active. Saving backfills
past occurrences immediately (the form warns how many); editing only affects future occurrences;
deleting keeps generated transactions.

**Loans (`#/loans`)** — list with principal / repaid / interest paid / remaining / progress bar;
detail modal with payment history, attachments and "+ Payment" (opens the transaction form
prefilled with the loan). The transaction form has a loan dropdown (expenses only) and an
interest-portion input with a suggested value of ≈ remaining × rate/12.

**Categories & tags (`#/labels`)** — list with usage counts, add / rename / delete.

**Rules (`#/rules`)**
- List of rules, one active
- Editor: name, textarea for script (monospace), "Run against year N" preview, save
- "Reset to default" restores the shipped Austrian rule

## 5b. Mobile & PWA

- Responsive CSS (`@media (max-width: 720px)`): logo-only header with horizontally scrolling nav, 2-column filters,
  transactions table rendered as cards, full-screen modal, ≥2.4rem touch targets.
- PWA via `vite-plugin-pwa` (`generateSW`, autoUpdate): `manifest.webmanifest` (standalone, theme `#ffea00`), icons
  generated from the logo (`ui/public/pwa-*.png`, maskable variants, apple-touch-icon), service worker precaches the
  built UI only; `/api/**` and `/_/**` are never cached (`navigateFallbackDenylist`). Go serves `.webmanifest`
  with `application/manifest+json`.

## 6. Tax scripting

Deliberately *not* encoding tax law in Go. The frontend aggregates the year and hands a plain object
to a user-supplied JS function body evaluated with `new Function('d', script)`. Single-user,
self-hosted, own code → acceptable; noted in README.

Input `d`:
```ts
{
  year: number,
  income: number, expenses: number, net: number,          // whole year
  area: { business: {income, expenses, net}, rental: {...}, private: {...} },
  category: { [name]: {income, expenses, net} },
  tag: { [name]: {income, expenses, net} },
  transactions: Array<{date, type, area, category, tags, amount}>,
  projected?: { income, expenses, net, area },  // still-planned recurring amounts of the year
}
```
Return value: `Array<{ label: string, value: number | string, hint?: string }>`.

Default shipped rule (rough Austrian freelancer + landlord, values editable in the script itself):
```js
// --- assumptions (edit freely) -------------------------------------------
const brackets = [ // Einkommensteuer tariff 2025 (rough; check current values)
  [13308, 0.00], [21617, 0.20], [35836, 0.30], [69166, 0.40],
  [103072, 0.48], [1000000, 0.50], [Infinity, 0.55],
];
const svRate = 0.2683;   // SVS: PV 18.5% + KV 6.8% + Selbständigenvorsorge 1.53%
const gfbRate = 0.15, gfbCap = 4950; // Grundfreibetrag (15% of first 33 000)
// -------------------------------------------------------------------------
const biz = d.area.business.net;
const rent = d.area.rental.net;
const sv  = Math.max(0, biz * svRate);
const gfb = Math.min(gfbCap, Math.max(0, biz) * gfbRate);
const taxable = Math.max(0, biz - sv - gfb) + Math.max(0, rent);
let tax = 0, prev = 0;
for (const [upTo, rate] of brackets) {
  if (taxable <= prev) break;
  tax += (Math.min(taxable, upTo) - prev) * rate;
  prev = upTo;
}
return [
  { label: 'Business profit',        value: biz },
  { label: 'Rental profit',          value: rent },
  { label: 'SVS contributions (est.)', value: -sv, hint: `${svRate*100}% of business profit` },
  { label: 'Gewinnfreibetrag',       value: -gfb },
  { label: 'Taxable income',         value: taxable },
  { label: 'Einkommensteuer (est.)', value: tax },
  { label: 'Effective rate',         value: (taxable ? tax/taxable*100 : 0).toFixed(1) + ' %' },
  { label: 'Set aside (SVS + ESt)',  value: sv + tax },
];
```
This is intentionally rough (no Sonderausgaben, AfA, Vorauszahlungen, USt…). Anyone can refine
their own copy without touching the app.

## 7. Backend specifics (`main.go`)

- `migrations/0001_init.go`: create `transactions` and `rules` (fields as in §4), insert default rule
  with `active=true`. Registered via `m.Register` so `go run . migrate` / auto-migrate on serve works.
- Superuser bootstrap on `OnServe`: if `FINANCE_ADMIN_EMAIL` + `FINANCE_ADMIN_PASSWORD` are set and
  no superuser exists → create it. Otherwise fall back to `./finance superuser upsert <email> <pw>`.
- Static: `e.Router.GET("/{path...}", apis.Static(uiFS, true))` with `uiFS = fs.Sub(embed, "ui/dist")`.
- Env: `PB_DATA=/pb_data` (data dir), default listen address `127.0.0.1:8070` (override with `--http`). Admin UI stays available at `/_/`.
- **Recurring generator** (`recurring.go`): materializes due template occurrences as transactions —
  daily cron `10 0 * * *` (UTC), a catch-up run on startup, and synchronously after a template is
  created/updated (so backfill shows up right away in the UI). Occurrence *n* is always computed
  from the template's start (`start + n·interval`, day-of-month clamped — Jan 31 → Feb 28 → Mar 31,
  no drift); `last_generated` is the dedup watermark, advanced atomically with the created rows, so
  user-deleted occurrences stay deleted. The same date math is mirrored in `ui/src/lib/recurring.ts`
  for the year projection (client only projects dates > today; server only writes ≤ today).
- Optional: nightly `pb_data` backup via PocketBase's built-in backups (config in admin UI) — no code.

## 7b. Migrations policy

Schema changes are **always a new numbered file** in `migrations/` (`0003_<name>.go`, …) that upgrades
an existing database in place — never edit an applied migration (PocketBase tracks them by filename in
`_migrations` and would silently skip the change). Migrations run automatically on `serve`.
`0002_multiuser.go` is the template: check-before-add, best-effort data carry-over, one-way.

## 8. Docker

```Dockerfile
FROM node:22-alpine AS ui
WORKDIR /ui
COPY ui/package*.json ./ && RUN npm ci
COPY ui . && RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./ && RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /finance .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /finance /finance
VOLUME /pb_data
EXPOSE 8070
ENTRYPOINT ["/finance", "serve", "--http=0.0.0.0:8070", "--dir=/pb_data"]
```
`docker-compose.yml`: one service, `./data:/pb_data`, env for admin bootstrap, `restart: unless-stopped`.

## 8b. CI / release

`.github/workflows/ci.yml` builds UI + Go on every push/PR. `release.yml` on a `v*` tag: cross-compiled binaries
(linux/darwin/windows, `-X main.version`) attached to a GitHub release + multi-arch image pushed to
`ghcr.io/floholz/murmelmoney:{version,major.minor,latest}`.

## 9. Implementation order

1. Go skeleton: PocketBase app + migration + superuser bootstrap → verify via admin UI (`/_/`)
2. Svelte skeleton: login gate, nav, `pb.ts`
3. Transactions page + form incl. file upload
4. Overview aggregates
5. Rules page + `runRule()` + default script
6. Embed UI, Dockerfile, compose, README (setup, backup, "the rule is JS you write yourself" caveat)

Rough size: ~300 lines Go, ~800 lines Svelte/TS. One or two evenings.

## 10. Open points / later ideas (not v1)
- CSV export for the Steuerberater
- Optional `vat_rate` field per transaction if USt ever becomes relevant
- Multiple active rules (e.g. compare scenarios) — trivial extension of the Rules page

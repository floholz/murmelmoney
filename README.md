<p align="center"><img src="assets/logo/murmelmoney-logo.svg" width="128" alt="murmelmoney logo"></p>

# murmelmoney

Minimal self-hosted personal finance tracker. Like a marmot (*Murmeltier*) stashing food for
winter — log what comes in and goes out, keep the receipts, and know roughly how much to put
aside for tax.

- add **incomes & expenses**, split into *business* / *rental* / *private*
- one **category** per transaction (Honorarnote, Software, Repairs…) and any number of **tags**
  (client, project, cost point, the house…) — both created on the fly
- attach **files** (receipts, invoices) and **notes** to every transaction
- yearly **overview** by area, category and tag
- **rough tax estimate** driven by a small JavaScript rule you can edit in the UI
  (ships with an Austrian freelancer + landlord example)
- multi-user (register / login, data strictly per user), single binary, single SQLite file —
  Go + [PocketBase](https://pocketbase.io) + Svelte

Explicitly *not*: bank sync, budgeting, multi-currency, real accounting.

## Run

```sh
docker compose up -d        # edit MURMEL_ADMIN_* in docker-compose.yml first
# → http://localhost:8090   (PocketBase admin UI at /_/)
```

Or the bare binary: `make build && MURMEL_ADMIN_EMAIL=… MURMEL_ADMIN_PASSWORD=… ./murmelmoney serve`.

Open the app and **register** your account (every user gets their own categories, tags,
transactions and tax rules). Set `MURMEL_REGISTRATION=false` once everyone is on board to close
sign-ups. `MURMEL_ADMIN_EMAIL` / `MURMEL_ADMIN_PASSWORD` optionally create the PocketBase superuser
for the admin UI at `/_/` (or `./murmelmoney superuser upsert <email> <pw>`).
Everything lives in `./data` (`/pb_data` in the container) — back that folder up
(PocketBase's built-in backups in the admin UI work too).

## Tax rules

The overview aggregates a year and passes it as `d` to the active rule, which is the *body of a JS
function* stored in the database and evaluated in your browser:

```js
d.year, d.income, d.expenses, d.net
d.area.business | rental | private   → { income, expenses, net }
d.category[name], d.tag[name]       → { income, expenses, net }
d.transactions[]                    → { date, type, area, category, tags, amount }
// return [{ label, value, hint? }, ...]   numbers render as €
```

The default rule is a deliberately rough Austrian estimate (Einkommensteuer brackets, ~27 % SVS,
Grundfreibetrag, rental profit added). Copy it, change the numbers, or write your own — the app
never encodes tax law itself. Since it's your own code running in your own browser, there is no
sandboxing.

## Develop

```sh
make dev                 # Go API + embedded UI on :8090
cd ui && npm i && npm run dev   # Vite dev server on :5173 with API proxy
```

`go build` embeds `ui/dist`, so run `make ui` (or `npm run build`) before building the binary.

## License

MIT

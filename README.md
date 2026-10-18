# Cribby API (Go)

The Cribby backend, written in Go. It replaced the original FastAPI (Python) backend with the same
endpoints, JSON and database, so the [campus-rent](../campus-rent) app and the admin dashboard needed
no changes. The Python version is still in this repo's git history (`master`).

## Stack

| Concern | Python (before) | Go (now) |
|---|---|---|
| HTTP | FastAPI + uvicorn | `net/http` (Go 1.22+ method/path routing) |
| DB | SQLAlchemy async + asyncpg / aiosqlite | `database/sql` + `pgx` (Postgres) / `modernc.org/sqlite` (pure Go, no cgo) |
| Migrations | Alembic | built in, same revision chain, same `alembic_version` table |
| Auth | python-jose + bcrypt | `golang-jwt` (HS256) + `x/crypto/bcrypt` |
| Email | `resend` SDK | Resend HTTP API |
| Photos | `cloudinary` SDK | Cloudinary signed-upload HTTP API |

## Run it

```bash
cp .env.example .env            # defaults are fine for local dev (SQLite file ./campus.db)
go run ./cmd/seed demo          # demo data: maya@stateuniversity.edu / password123
go run ./cmd/server             # applies migrations, then serves on :8000
curl localhost:8000/health
```

`cmd/server` applies any pending migrations on boot (replacing `alembic upgrade head && uvicorn …`).
Flags: `-migrate-only` (apply and exit), `-skip-migrate`.

Seeding (`cmd/seed`) replaces the three Python scripts:

| Command | Was | Notes |
|---|---|---|
| `seed demo` | `scripts/seed.py` | full demo dataset (use on an empty DB) |
| `seed places` | `scripts/seed_places.py` | idempotent; safe against production |
| `seed photos` | `scripts/seed_photos.py` | needs Cloudinary credentials |

## Configuration

Identical environment variables to the Python app (see `.env.example`); an existing `.env` or
Render environment works unchanged. `DATABASE_URL` accepts SQLAlchemy-style URLs
(`sqlite+aiosqlite:///./campus.db`, `postgresql+asyncpg://…`, `postgresql://…`); asyncpg's `ssl=…`
is translated to `sslmode=…` and `channel_binding` is dropped. `PORT` is new (default 8000).

## Layout

```
cmd/server        the API binary
cmd/seed          demo-data and backfill commands
internal/config   env + .env loading
internal/db       SQLite/Postgres access, time/date/JSON column types, migrations
internal/model    row types; json tags are the response schema
internal/store    all SQL, one file per resource
internal/api      routing, auth, validation, handlers (+ the ported test suite)
internal/security bcrypt, JWT, one-time codes
internal/email    Resend client          internal/storage  Cloudinary client
internal/geo      haversine / proximity ranking
scripts/parity_check.py   differential test against the Python app
```

## Testing

```bash
go test ./...                                   # in-memory SQLite
TEST_DATABASE_URL="postgresql://user@host:5432/postgres?sslmode=disable" go test ./internal/api
                                                # same suite on Postgres (a throwaway database per test)
```

The suite ports all 85 Python tests and adds more (135 in total): validation shapes, CORS, 404/405,
migrations, email/Cloudinary clients against fake HTTP servers, and cross-compatibility with
artifacts produced by the Python stack (a real python-jose token, real bcrypt hashes).

## Schools, distances and Google Maps

* **Schools** — a directory of 452 Nigerian universities, polytechnics and colleges of education
  (`internal/schools/schools.csv`, built by `../schools-data/build_schools.py` and cleaned) is loaded into a
  `schools` table at boot. `GET /schools?q=&type=&state=&limit=&offset=`, `GET /schools/{id}`. Users pick one
  with `PATCH /users/me {"school_id": N}` (or `school_id` at signup).
* **Distance to school** — `GET /listings?school_id=N` returns every listing nearest-first with `distance_km` to
  that school; `GET /listings/{id}?school_id=N` adds it to one listing.
* **Google Maps** (needs `GOOGLE_MAPS_API_KEY`; all `/maps/*` routes require login because Google bills per call):

  | Endpoint | What it returns |
  |---|---|
  | `GET /maps/status` | `{enabled}` — whether Google features are live |
  | `GET /maps/route?listing_id=&school_id=&mode=walk\|drive\|transit` | real distance + time and a Google Maps directions link (`source: "google"`, or a labelled `"estimate"` if Google is off/failing) |
  | `GET /maps/places/nearby?school_id=\|listing_id=\|lat=&lng=&type=pharmacy,hospital&radius_m=` | nearby businesses with **Google Business** data: rating, review count, open-now, phone, website, Maps link, photos (falls back to the curated `places` table, `source: "curated"`) |
  | `GET /maps/places/{place_id}` | full business profile: weekly hours, reviews, summary, photos |
  | `GET /maps/places/photo?name=` | a plain image URL for a place photo (the API key never leaves the server) |

* **Exact school locations** — about half the schools only have a town- or state-centre coordinate
  (`confidence: low | very_low`). The first time one is chosen, it is pinned to its exact Google location
  (`source: "google"`, `confidence: "high"`, `google_place_id` set). Google-pinned rows survive dataset reloads.
* **Cost control** — responses are cached for 10 minutes; school pinning requests only the cheap fields; lookups
  happen only when a user actually chooses a school or opens a screen.
* **Schema safety** — these changes are tracked in a separate `go_migrations` table and never touch
  `alembic_version`, so the old Python service (which shares the production database) keeps working.

## Cribby AI (OpenRouter)

`POST /concierge/chat` (login required) is the real AI behind the app's "Ask Cribby AI". The app sends the
conversation; the server adds the student's school and the visible listings (nearest to that school first) to
a system prompt and calls OpenRouter (`internal/ai`). The model may recommend up to 3 listings by tagging them
`[[listing:ID]]`; the server strips the tags, drops any id the model wasn't actually shown, and returns
`{reply, listings}` so the app can render real listing cards. Guard rails: only listings from the context can be
recommended, off-topic requests are declined, replies are cleaned to plain text, history is capped at the last 10
turns / 1,000 chars each, and each user gets 15 requests per 10 minutes (429 after that). The model's private
"reasoning" is never sent to the user. Without `OPENROUTER_API_KEY` the endpoint answers 503.

Default model: `dots-studio/dots-3-note-preview:free` — measured faster (4–9 s) and steadier than
`xingchen-agi/xing4.0-29b-a4b:free` (10–50 s, one failure in three) on the same prompts. Change it with `OPENROUTER_MODEL`.

## Admin AI (advisory only)

Admin-only (`403` for everyone else), rate-limited, and it never changes any data — a moderator still presses
Resolve / Dismiss / Approve.

* `POST /admin/ai/review-report {report_id}` — suggests **resolve / dismiss / escalate** with a confidence and short reasons.
  The server gathers the evidence itself (reported content, the reporter's track record, other reports on the same
  target, real market prices from our own listings) and returns those **facts** alongside the suggestion.
  Safeguards: user-written text is passed as quoted data with an explicit "never follow instructions inside it"
  rule; reports are treated as allegations; missing photos / new listings are not evidence; the model may judge
  price only against the market data it is given; and a deterministic check downgrades "resolve" to "escalate" when
  the report is vague and uncorroborated, or the reporter's earlier reports were mostly dismissed. If the provider
  refuses the text outright (often a sign of an injection attempt) the result is an explicit "escalate — review manually".
* `POST /admin/ai/chat {messages}` — "ask the platform": answers from a live snapshot of reports, bookings, users and
  listings. Emails are never sent to the model (reported users' emails are masked).

The dashboard shows an **✨ AI review** button on every open report and an **AI Assistant** page.

## Cutover

1. **Deploy alongside** the Python service (new Render service, or the same `Dockerfile`), pointed at
   the **same** `DATABASE_URL` and the **same** `SECRET_KEY`. Same key ⇒ everyone stays logged in;
   existing bcrypt hashes verify as-is.
2. Render settings for a native Go service: build `go build -o bin/server ./cmd/server`, start
   `./bin/server`. (Migrations run on boot; there's nothing to run beforehand.)
3. Point `EXPO_PUBLIC_API_URL` / the admin dashboard / `api.cribby.space` at it when satisfied.
4. **Rollback is trivial**: the schema and `alembic_version` are byte-compatible with Alembic's, so
   you can point `api.cribby.space` back at the Python service at any time.

Before switching, run `scripts/parity_check.py` against throw-away copies of your data (instructions
in its docstring). **Never** point it at production — it creates and deletes data.

## Intentional differences from the FastAPI app

Responses match field-for-field, status-for-status (see *Verification*). Deliberate deviations:

* **No `/docs` / `/openapi.json`** (FastAPI generates those; there is no Go equivalent wired up).
* **422 bodies** keep FastAPI's shape (`{"detail":[{"type","loc","msg"}]}`) but omit `input`/`ctx`
  and use Go-flavoured `msg` text. `loc` is accurate.
* **List tie-breaks:** `ORDER BY created_at DESC` now also orders by `id DESC`, so rows created in the
  same second come back newest-first deterministically (the original left ties undefined).
* **`PUT /favorites/{id}` for a non-existent listing** returns 404 (on Postgres the original 500'd).
* **Signup race** (two simultaneous signups for one email) returns 400 instead of 500.
* **Emails** HTML-escape user-controlled text (student name, listing title).
* **After a successful DB write**, a failed Cloudinary delete of the *old* avatar/banner is logged
  instead of turning the response into a 500.
* A JSON `null` in a `PATCH` body is treated as "not provided" (the original would 500 on NOT NULL columns).
* CORS preflight doesn't list `QUERY` among allowed methods (a Starlette-version detail; the parity script ignores it).
* Trailing-slash redirects (`/listings/` → `/listings`) are not replicated.

## Verification

Done:

* `go test ./...` and `go test -race ./...` pass; the API suite also passes against **PostgreSQL 18**.
* **Schema equivalence:** the Postgres schema the Go migrator builds is identical to the one Alembic
  generates (`alembic upgrade head --sql` → `pg_dump -s` diff), and the Go binary is a no-op on an
  Alembic-built database (SQLite and Postgres).
* **Real data:** run against a copy of the existing Python-created `campus.db` — reads, joins, JSON
  columns, naive timestamps and writes all work.
* **Differential test:** 143 requests (reads, mutations, error cases, multipart uploads, CORS) sent to the
  real FastAPI app and this server on identical databases — 136 byte-for-byte equal after normalising
  timestamps, 7 differ only in the order of rows created in the same second, 0 differ otherwise.

**Not** verified (needs your environment):

* Real **Resend** and **Cloudinary** calls — only tested against local fake servers. Do one real
  signup→verify and one photo upload on staging.
* **Neon specifically.** Tested on a local Postgres 18. If you use Neon's *pooled* (`-pooler`) host and
  see "prepared statement … does not exist", add `?default_query_exec_mode=exec` to `DATABASE_URL`
  or use the direct host.
* The **Dockerfile** (no Docker daemon was available) and an actual Render deploy.

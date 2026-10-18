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

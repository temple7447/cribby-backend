# Running the Cribby API in production

## Required settings

| Variable | Notes |
|---|---|
| `DATABASE_URL` | Postgres in production. |
| `SECRET_KEY` | **Required.** The server refuses to start in production (Postgres or `ENV=production`) if this is missing, is the public default, or is shorter than 32 characters. Generate one with `openssl rand -hex 32`. Keep the same value across deploys, or everyone is signed out. |
| `RESEND_API_KEY`, `EMAIL_FROM` | Without the key, verification, reset, booking and message emails are only logged, never sent. |
| `CLOUDINARY_*` | Photo and video uploads answer 503 without them. |
| `GOOGLE_MAPS_API_KEY`, `OPENROUTER_API_KEY` | Maps/places and the AI concierge. |
| `BACKEND_CORS_ORIGINS` | If set, it **replaces** the built-in list, so include `https://www.cribby.space`, `https://cribby.space`, `https://admin.cribby.space`. |

`server -migrate-only` works without `SECRET_KEY` (it only applies migrations).

## Uptime monitoring and cold starts

Two endpoints:

* `GET /health` — the process is up (instant, no database).
* `GET /health/ready` — the database answers too. **Point your monitor at this one.** `503` means the database is unreachable.

On Render's free plan the service sleeps after ~15 minutes without traffic and the next request waits 30–50 s. Options:

1. **Free monitor that also keeps it awake:** create a monitor in UptimeRobot (or Better Stack / cron-job.org) for `https://api.cribby.space/health/ready` every **5 minutes**, with e-mail alerts on failure. This prevents the sleep and tells you when the API is down.
2. **Paid instance** (Render "Starter" or above): no sleeping at all. The right answer once students depend on it.

The app already retries over ~70 s so a cold start works, but it feels slow.

## Backups

```bash
# always set DATABASE_URL explicitly so a backup can never hit the wrong database
DATABASE_URL='postgresql://…' go run ./cmd/backup -out /secure/backups -keep 14
```

* SQLite (local): a consistent snapshot via `VACUUM INTO`. Postgres: `pg_dump` custom format (needs the PostgreSQL client tools, at least as new as the server).
* The tool checks the file is readable before keeping it and refuses an empty dump. Files are `0600`, the folder `0700`, and `backups/` is git-ignored.
* Schedule it daily (cron on a machine you control, or a Render Cron Job). Copy the files off that machine.
* **Backups contain everyone's data** (emails, password hashes, messages). Store them privately and encrypt before they leave the machine, e.g. `age -p -o file.dump.age file.dump`. Do **not** put them in GitHub.
* Neon also offers point-in-time recovery on paid plans; use it as a second layer, not instead of this.

Test a restore into a **new, empty** database before you ever need one:

```bash
createdb cribby_restore_test
go run ./cmd/restore -in backups/cribby-backup-….dump -url 'postgresql://…/cribby_restore_test' -yes
```

Restoring over production: `go run ./cmd/restore -in <file> -yes` (target is `DATABASE_URL`). It **replaces** the data; it prints the target and does nothing without `-yes`.

## Abuse protection (in-memory, per instance)

| What | Limit |
|---|---|
| Failed sign-ins | 5 per account per address, 20 per account overall, 40 per address, in 15 min. A correct sign-in clears the account's count. |
| Wrong verification / reset codes | 5 per account in 15 min |
| Sign-ups | 30 per address per hour |
| Password-reset e-mails | 15 per address per hour |
| App/website error reports | 60 per address per hour |
| AI concierge | 15 per user per 10 min |

The limits reset when the server restarts. If you ever run more than one instance, move them to a shared store (Redis).

## Error reports

The app and website send crashes to `POST /api/v1/client-errors`; admins read them under **App errors** in the admin dashboard. Identical errors are grouped with a count, and an error that comes back after being resolved reopens.

## Continuous integration

`.github/workflows/ci.yml` runs gofmt, `go vet`, build and the tests on every push and pull request, and the API/backup/db tests again on Postgres 16. Make it a required check in the GitHub branch settings so a failing change can't be merged.

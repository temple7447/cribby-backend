# Cribby API

FastAPI backend for [campus-rent](../campus-rent), the Cribby student housing app.

## Stack

FastAPI + SQLAlchemy 2.0 (async) + Alembic + JWT auth (python-jose + bcrypt). Local dev uses SQLite
(`campus.db`, zero setup); set `DATABASE_URL` to a Postgres URL for production (e.g. Render/Neon —
see `app/core/config.py`).

## Setup

```bash
uv sync --extra dev
uv run alembic upgrade head
uv run python scripts/seed.py   # demo data: 6 listings, 4 roommates, maya@stateuniversity.edu / password123
uv run uvicorn app.main:app --reload --port 8000
```

API docs at `http://localhost:8000/docs`.

## Project layout

```
app/
  core/        settings (config.py), password hashing + JWT (security.py)
  db/          engine/session, declarative Base, UTCDateTime column type
  models/      SQLAlchemy models
  schemas/     Pydantic request/response models
  crud/        DB access per resource
  api/v1/      routers + endpoints, deps.py (get_current_user)
alembic/       migrations
scripts/seed.py
```

## Connecting the frontend

`campus-rent` reads `EXPO_PUBLIC_API_URL` from its `.env` (see `.env.example` there). Defaults to
`http://localhost:8000/api/v1`, which works for web and the iOS simulator. Android emulator needs
`http://10.0.2.2:8000/api/v1`; a physical device needs your machine's LAN IP.

## What's not wired up yet

- OAuth (Apple/Google) — the buttons in the app show a "coming soon" notice
- AI Concierge and Lease Explainer are still client-side simulations (no LLM integration) — the
  Concierge does search real listings via `/listings`, but the reasoning is a canned heuristic
- Real-time push notifications for new messages/booking updates

# go-template

Full-stack auth template: **Next.js → Go API → PostgreSQL**. The browser only
ever talks to Next.js; the Go API is proxied server-side and never exposed
directly.

```
Browser
   ↓
Next.js        frontend/  → :3000   React UI, routing, SSR/static gen, server components
   ↓
Go API         backend/   → :8080   chi + pgx + JWT + scrypt + email worker
   ↓
PostgreSQL     migrations apply on boot
```

## Quick Start

```bash
./manage.sh       # → [7] First-Time Setup, then → [1] Start All
```

Needs Go 1.22+, Node 20+, and a running PostgreSQL (option 1 refuses to start
if it's down). Dev admin: **admin@mail.com** / **Password1234!**.

## Docs

- **[docs/FEATURE.md](docs/FEATURE.md)** — what this build does
- **[docs/DATABASE.md](docs/DATABASE.md)** — install Postgres, schema, reset, tests
- **`.env.example`** — every config variable

## Tests

`./manage.sh` → 6 runs backend tests + frontend build. Backend integration
tests need `TEST_DATABASE_URL` (see docs/DATABASE.md).

## Roles

`client` < `staff` < `admin`. Grant via CLI only (no self-service promotion):

```bash
cd backend && go run ./cmd/set-role you@email.com admin
# or: ./manage.sh → [8]
```

## API

17 endpoints under `/api/*` — see `backend/app.go` (`routes()`).

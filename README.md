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

### Prerequisites

- **Go 1.22+**, **Node 20+**, and a running **PostgreSQL**.
- The backend refuses to start if Postgres is down. On macOS:
  `brew services start postgresql@16`.

### 1. Install dependencies

```bash
./manage.sh   # → [7] First-Time Setup (npm install + go mod download)
```

### 2. Start everything

```bash
./manage.sh   # → [1] Start All
```

Backend runs on **:8080**, frontend on **:3000**. Logs:
`/tmp/go-template-backend.log`, `/tmp/go-template-frontend.log`.

Open **http://localhost:3000** and log in with the dev admin:
**admin@mail.edu** / **Password1234!**.

Users can log in with either their **email** or their **UID** (university ID).
After `LOGIN_MAX_ATTEMPTS` (default 5) failed logins, that account is locked
for `LOGIN_LOCK_MINUTES` (default 15) minutes.

### Two-factor authentication

Logging in from a **new device** requires a 4-digit code sent to the user's
preferred communication channel (`text`/`phone` → SMS, otherwise email). Once
a device verifies, it's remembered and skips 2FA on later logins. SMS is
logged to stdout (no provider is wired up); email goes through the normal
queue. In **development** the code is always `1234` so testing needs no mail
server.

### 3. Stop / status

```bash
./manage.sh   # → [4] Stop All, [5] Status
```

### Trying the schedule system

The dev admin is an `admin`, so it lands on `/admin` where you can create
locations, departments, and assign managers to locations. To try the other
roles, create users (admin → Add User) and promote them via the CLI:

```bash
cd backend && go run ./cmd/set-role student@mail.edu student
cd backend && go run ./cmd/set-role manager@mail.edu manager
```

Then log in as each to see `/student` and `/manager`.

### Seed data

On a fresh database, the backend also seeds the accounts in **`backend/seed/seed.csv`**
at startup — a manager plus a few students across departments and locations.
Edit the file and restart the backend to change them (existing emails are
skipped). To wipe and re-seed in one step, use `./manage.sh → [11]` (Re-seed).
Columns:
`email,password,firstname,lastname,address1,address2,city,state,zip,country,role,department,department_code,location,location_abbr,location_address,location_city,location_state,location_zip,location_country,phone,min_hours,max_hours`.

## Running with Docker

Prefer containers over the local `manage.sh` workflow? The repo ships a
`docker-compose.yml` plus a `Makefile` of shortcuts. Full details are in
**[docs/DOCKER.md](docs/DOCKER.md)**.

### Switch to Docker

1. **Copy the Docker env file** — it uses non-conflicting ports so Docker can
   run alongside `manage.sh`. `.env.docker` is personal config (not in the
   repo) — ask a teammate for a copy, or create `.env` from `.env.example`
   with these docker overrides:

   This maps the frontend to **:3001**, backend to **:8081**, and Postgres to
   **:5433**. To use the default ports instead, edit `.env` and set
   `FRONTEND_PORT=3000`, `BACKEND_PORT=8080`, `POSTGRES_PORT=5432`.

2. **Build and start:**

   ```bash
   make docker-up
   # or: docker-compose up --build
   ```

3. **Open the app** at **http://localhost:3001** and log in with the dev admin
   **admin@mail.edu** / **Password1234!**.

### Everyday commands

| Task                 | Command               |
| -------------------- | --------------------- |
| Start                | `make docker-up`      |
| Stop                 | `make docker-down`    |
| Status               | `make docker-status`  |
| Follow logs          | `make docker-logs`    |
| Rebuild from scratch | `make docker-rebuild` |
| Re-seed database     | `make docker-reseed`  |

### Switching back to local

Stop the containers (`make docker-down`) and use `./manage.sh` as before. The
two workflows share the same codebase and can coexist because Docker uses
different ports.

## Demo video

`HTT-Scheduling-Demo.mp4` at the project root is built from the live app by
`scripts/demo/`:

1. `node scripts/demo/reset-demo.mjs` — resets demo-mutated state via the API
   and re-adds the prep data (safe to re-run; no database reset needed)
2. `node scripts/demo/record.mjs` — records the nine scenes (signup, login +
   2FA, student, full-time/hourly staff, admin + RBAC, onboarding, department
   manager, scheduler, audit) with the app running on :3000
3. `./scripts/demo/build.sh` — narration (neural TTS, warm American male
   voice), watermark, title cards, and the final concat

Narration text lives in `scripts/demo/narration.txt` (one line per scene).

## Docs

- **[docs/FEATURE.md](docs/FEATURE.md)** — what this build does
- **[docs/DATABASE.md](docs/DATABASE.md)** — install Postgres, schema, reset, tests
- **`.env.example`** — every config variable

## Tests

`./manage.sh` → 6 runs backend tests + frontend build. Backend integration
tests need `TEST_DATABASE_URL` (see docs/DATABASE.md).

## Roles

`student` < `staff` < `manager` < `admin`. Grant via CLI only (no
self-service promotion):

```bash
cd backend && go run ./cmd/set-role you@email.edu admin
# or: ./manage.sh → [8]
```

## Student Work-Schedule System

Students, managers, and admins get role-specific pages (`/student`, `/manager`,
`/admin`) after login. Admins set up locations/departments and can one-click
disable any account. Managers assign students to a department with min/max
weekly hours, set preset weekly schedules, drop shifts into the workqueue, and
approve/deny requests (including hour-overflow overrides). Students see their
own calendar and their department's workqueue, pick shifts (capped at max
hours), set preferred days/times, and request to miss a scheduled shift (which
returns it to the workqueue).

Admins can also **Reset Password** on any Access Control row: it generates a
15-character temporary password, emails it to the user, and forces a change on
next login (the user enters the emailed password as their current password).

### AI assistant (Admin/Manager)

The Admin and Manager screens include a Zed-style AI assistant panel. Its system
prompt and provider are pre-configured via environment variables (keep the key
server-side — the browser only ever talks to the backend proxy):

- `AI_PROMPT` — the pre-configured system prompt given to the agent.
- `AI_BASE_URL` — base URL of any OpenAI-compatible provider (e.g.
  `https://api.openai.com/v1`, or a self-hosted Ollama/LM Studio endpoint).
- `AI_API_KEY` — provider API key (server only).
- `AI_MODEL` — model name, defaults to `gpt-4o-mini`.

If `AI_API_KEY`/`AI_BASE_URL`/`AI_MODEL` are unset, the panel shows the
"not configured" message instead of failing. Only the `admin` and `manager`
roles see it.

## API

17 endpoints under `/api/*` — see `backend/app.go` (`routes()`).

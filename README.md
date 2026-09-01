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

On a fresh database, the backend also seeds the accounts in **`backend/seed.csv`**
at startup — a manager plus a few students across departments and locations.
Edit the file and restart the backend to change them (existing emails are
skipped). To wipe and re-seed in one step, use `./manage.sh → [11]` (Re-seed).
Columns:
`email,password,firstname,lastname,address1,address2,city,state,zip,country,role,department,department_code,location,location_abbr,location_address,location_city,location_state,location_zip,location_country,phone,min_hours,max_hours`.

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

## API

17 endpoints under `/api/*` — see `backend/app.go` (`routes()`).

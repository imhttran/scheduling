# Database (PostgreSQL)

Everything this project does with PostgreSQL, in one page.

## Connection

One variable: `DATABASE_URL` (a personal `.env` or `.env.dev` provides the
connection; neither is in the repo — copy the values from `.env.example` and
fill in your local credentials).

```
postgres://postgres:postgres@localhost:5432/go_template?sslmode=disable
```

- `manage.sh` (reset-database, backend startup check) reads `.env` first, then
  `.env.dev` — same precedence as the Go backend's environment loader.
- Changing host/port/db means changing only this URL — no code changes.

## Setting up a local instance (macOS)

**Option 1 — Homebrew service (recommended): survives reboots**

```bash
brew install postgresql@16
brew services start postgresql@16      # stop with: brew services stop postgresql@16
createuser -s postgres; psql -d postgres -c "ALTER USER postgres PASSWORD 'postgres';"
createdb go_template
```

**Option 2 — throwaway instance (no service installed): lost on reboot**

```bash
initdb -D /tmp/go-template-pg -A trust
pg_ctl -D /tmp/go-template-pg -l /tmp/go-template-pg.log start
psql -d postgres -c "CREATE USER postgres WITH PASSWORD 'postgres' SUPERUSER;"
createdb go_template -U postgres
```

Check state anytime: `pg_isready -h localhost` (this is what `manage.sh` runs
before launching the backend).

## Schema: how it's managed

Embedded migrations (`backend/migrations/00N_*.sql`), applied automatically
when the Go API boots:

- `main.go` runs each version's statements, then records the version in a
  `schema_migrations` table — a second boot is a no-op, so no separate
  migrate step and no `migrate down`.
- New schema changes get their own `00N_*.sql` file plus an entry in the
  runner loop in `main.go` (and in `ensureTestSchema` in `app_test.go`).

Tables:

| Table                            | Purpose                                                                 |
| -------------------------------- | ----------------------------------------------------------------------- |
| `users`                          | accounts: email, scrypt password, roles, verify/reset tokens            |
| `user_profiles`                  | one-time registration details (`ON DELETE CASCADE`)                     |
| `email_queue`                    | outbound mail (processed by the worker in `backend/queue.go`)           |
| `departments`                    | widest scope group (was `locations`); `manager_id` = Department Manager |
| `teams`                          | smallest scope group (was `departments`); `manager_id` = team's manager |
| `worker_teams`                   | worker → team membership (a worker may be in several teams)             |
| `jobs` / `student_jobs`          | work-function layer: qualifications + per-job hour caps                 |
| `workqueue`                      | all shifts (`team_id`, `job_id`, split groups via `parent_shift_id`)    |
| `schedule_requests`              | worker miss/overflow requests                                           |
| `job_schedules` / `job_holidays` | per-job operating hours and closures                                    |
| `audit_log`                      | append-only trail of request/shift/job actions, scoped by `team_id`     |

All scheduling tables live in the `scheduling` Postgres schema; `DATABASE_URL`
carries `search_path=scheduling` (defaults include it; the runner creates the
schema if missing).

Dev seed (in `backend/main.go`, only when `NODE_ENV=development`): upserts
`admin@mail.edu` / `Password1234!` plus their profile, so the dev admin isn't
blocked by onboarding gates.

## Day-to-day operations

| Task               | Command                                                                                         |
| ------------------ | ----------------------------------------------------------------------------------------------- |
| Status             | `pg_isready -h localhost` or `./manage.sh` → 5                                                  |
| Reset **all** data | `./manage.sh` → 9 (drops and recreates the `scheduling` schema)                                 |
| Manual reset       | `psql "$DATABASE_URL" -c 'DROP SCHEMA IF EXISTS scheduling CASCADE; CREATE SCHEMA scheduling;'` |
| Look around        | `psql go_template -U postgres` → `SET search_path TO scheduling;` then `\dt`                    |
| Promote a user     | `./manage.sh` → 8 (runs `backend/cmd/set-role`)                                                 |

`manage.sh` option 9 asks for lowercase `yes` since `DROP SCHEMA scheduling
CASCADE` destroys all data. It reads the same `DATABASE_URL` chain described
above.

## Tests

`backend/app_test.go` integration tests need a reachable Postgres and are
skipped otherwise:

```bash
cd backend
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/go_template?sslmode=disable&search_path=scheduling" go test ./...
```

The tests create unique-email users per run and never reset the database.
`./manage.sh` → 6 runs them when `TEST_DATABASE_URL` is set in your
environment.

## Production

Any managed PostgreSQL (RDS, Cloud SQL, Neon, a Docker container) works: set
`DATABASE_URL` in the environment (`NODE_ENV=production` loads no `.env.dev`,
and only the backend's server environment matters — the frontend never
touches Postgres). Migrations apply on first boot against an empty database.

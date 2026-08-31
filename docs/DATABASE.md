# Database (PostgreSQL)

Everything this project does with PostgreSQL, in one page.

## Connection

One variable: `DATABASE_URL` (`.env.dev` already provides the dev default; a
personal root `.env` overrides it).

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

One embedded migration (`backend/migrations/001_init.sql`), applied
automatically when the Go API boots:

- `main.go` runs every `CREATE TABLE IF NOT EXISTS` statement, then records
  the version in a `schema_migrations` table — a second boot is a no-op, so
  no separate migrate step and no `migrate down`.
- There are **no per-feature migration files yet**. Schema changes today go
  into `001_init.sql` directly (safe because `IF NOT EXISTS`), or — for the
  cleaner path — add a `002_*.sql` and extend the small runner loop in
  `main.go` before the schema drifts.

Tables:

| Table           | Purpose                                                       |
| --------------- | ------------------------------------------------------------- |
| `users`         | accounts: email, scrypt password, role, verify/reset tokens   |
| `user_profiles` | one-time registration details (`ON DELETE CASCADE`)           |
| `email_queue`   | outbound mail (processed by the worker in `backend/queue.go`) |

Dev seed (in `backend/main.go`, only when `NODE_ENV=development`): upserts
`admin@mail.com` / `Password1234!` plus their profile, so the dev admin isn't
blocked by onboarding gates.

## Day-to-day operations

| Task               | Command                                                                       |
| ------------------ | ----------------------------------------------------------------------------- |
| Status             | `pg_isready -h localhost` or `./manage.sh` → 5                                |
| Reset **all** data | `./manage.sh` → 9 (drops and recreates the `public` schema)                   |
| Manual reset       | `psql "$DATABASE_URL" -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'` |
| Look around        | `psql go_template -U postgres` → `\dt`, `\d users`                            |
| Promote a user     | `./manage.sh` → 8 (runs `backend/cmd/set-role`)                               |

`manage.sh` option 9 asks for lowercase `yes` since `DROP SCHEMA public
CASCADE` destroys all data. It reads the same `DATABASE_URL` chain described
above.

## Tests

`backend/app_test.go` integration tests need a reachable Postgres and are
skipped otherwise:

```bash
cd backend
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/go_template?sslmode=disable" go test ./...
```

The tests create unique-email users per run and never reset the database.
`./manage.sh` → 6 runs them when `TEST_DATABASE_URL` is set in your
environment.

## Production

Any managed PostgreSQL (RDS, Cloud SQL, Neon, a Docker container) works: set
`DATABASE_URL` in the environment (`NODE_ENV=production` loads no `.env.dev`,
and only the backend's server environment matters — the frontend never
touches Postgres). Migrations apply on first boot against an empty database.

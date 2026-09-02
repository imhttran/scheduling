-- Full schema for the scheduling system. Single greenfield migration — no
-- back-compat: dev databases are wiped (manage.sh → 9) and re-applied from
-- scratch. Objects live in the `scheduling` schema; DATABASE_URL must carry
-- search_path=scheduling (the runner creates the schema if missing).

CREATE SCHEMA IF NOT EXISTS scheduling;

-- Highest-ranked held role. `roles` is the source of truth; `role` is a
-- generated convenience column so rank-based queries (role <> 'admin',
-- role IN ('student','staff'), ORDER BY role, ...) keep working.
-- Ladder: student < staff < manager < admin.
CREATE OR REPLACE FUNCTION top_role(roles TEXT[]) RETURNS TEXT
LANGUAGE sql IMMUTABLE AS $$
  SELECT r FROM unnest(roles) r
  ORDER BY array_position(ARRAY['student','staff','manager','admin'], r) DESC NULLS LAST
  LIMIT 1
$$;

CREATE TABLE IF NOT EXISTS users (
  id                  SERIAL PRIMARY KEY,
  email               TEXT NOT NULL UNIQUE,
  uid                 TEXT UNIQUE,
  password            TEXT NOT NULL,
  roles               TEXT[] NOT NULL DEFAULT ARRAY['student']::text[],
  role                TEXT GENERATED ALWAYS AS (top_role(roles)) STORED,
  -- How a worker's weekly hours are governed: 'student' | 'fulltime' | 'hourly'.
  -- hourly_limit: manager-set regular weekly hours for an 'hourly' worker
  -- (1..40, where 40 is the default). NULL means use the 40 default.
  worker_type         TEXT NOT NULL DEFAULT 'student',
  hourly_limit        INTEGER,
  email_verified      BOOLEAN NOT NULL DEFAULT false,
  must_change_password BOOLEAN NOT NULL DEFAULT false,
  disabled            BOOLEAN NOT NULL DEFAULT false,
  verification_token  TEXT UNIQUE,
  reset_token         TEXT UNIQUE,
  reset_token_expiry  TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One-time registration details. A missing row (not a boolean flag, unlike
-- must_change_password) is what gates a user into the completion form — the
-- data itself is the "is this done" signal, so there's nothing to keep in sync.
CREATE TABLE IF NOT EXISTS user_profiles (
  id                       SERIAL PRIMARY KEY,
  user_id                  INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  first_name               TEXT NOT NULL,
  last_name                TEXT NOT NULL,
  address                  TEXT NOT NULL,
  address2                 TEXT,
  city                     TEXT,
  state                    TEXT NOT NULL,
  zip                      TEXT NOT NULL,
  country                  TEXT NOT NULL DEFAULT 'US',
  phone                    TEXT NOT NULL,
  communication_preference TEXT NOT NULL DEFAULT 'email',
  linkedin                 TEXT,
  github                   TEXT,
  alt_email                TEXT
);

CREATE TABLE IF NOT EXISTS email_queue (
  id         SERIAL PRIMARY KEY,
  "to"       TEXT NOT NULL,
  subject    TEXT NOT NULL,
  body       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  attempts   INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at    TIMESTAMPTZ
);

-- Trusted devices (skip 2FA on login) and pending 2FA login codes.
CREATE TABLE IF NOT EXISTS user_devices (
  id         SERIAL PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id  TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS login_codes (
  id         SERIAL PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token      TEXT NOT NULL UNIQUE,
  code       TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  used       BOOLEAN NOT NULL DEFAULT false,
  attempts   INTEGER NOT NULL DEFAULT 0,
  resends    INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Failed-login tracking for rate limiting (keyed by email/UID).
CREATE TABLE IF NOT EXISTS login_attempts (
  id           SERIAL PRIMARY KEY,
  identifier   TEXT NOT NULL UNIQUE,
  attempts     INTEGER NOT NULL DEFAULT 0,
  locked_until TIMESTAMPTZ,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- Work-schedule system ----
-- Hierarchy: department → teams → workers. Scope is a manager_id column on
-- the entity being managed (no assignment side-tables): a department's
-- manager oversees every team in it, a team's manager runs that team. A
-- manager may hold several teams and/or departments (rows point at them).

-- Departments: the widest scoped group (was "locations" in older vocabulary).
CREATE TABLE IF NOT EXISTS departments (
  id           SERIAL PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  abbreviation TEXT,
  address      TEXT,
  address2     TEXT,
  city         TEXT,
  state        TEXT,
  zip          TEXT,
  country      TEXT,
  manager_id   INTEGER REFERENCES users(id) ON DELETE SET NULL
);

-- Teams: the smallest scoped group (was "departments" in older vocabulary).
-- The workqueue is the single source of truth for shifts; weekly_schedules is
-- a template the manager sets that generates the current week's shifts.
CREATE TABLE IF NOT EXISTS teams (
  id            SERIAL PRIMARY KEY,
  department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  team_code     TEXT,
  description   TEXT,
  active        BOOLEAN NOT NULL DEFAULT true,
  manager_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  UNIQUE (department_id, name)
);

-- A worker's team membership — one row per team they belong to; a worker may
-- be in several teams. Jobs (student_jobs) are the work-function layer on
-- top; hour caps live there, not here.
CREATE TABLE IF NOT EXISTS worker_teams (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  active  BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (user_id, team_id)
);

-- Preset weekly template (0=Sunday..6=Saturday). Generates workqueue rows.
CREATE TABLE IF NOT EXISTS weekly_schedules (
  id          SERIAL PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day_of_week INTEGER NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL
);

-- Worker's preferred days/times (informational, not enforced).
CREATE TABLE IF NOT EXISTS preferred_times (
  id          SERIAL PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day_of_week INTEGER NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL
);

-- A job is a position a worker is qualified to do, belonging to one team.
CREATE TABLE IF NOT EXISTS jobs (
  id              SERIAL PRIMARY KEY,
  name            TEXT NOT NULL,
  team_id         INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  optimal_workers INTEGER NOT NULL DEFAULT 1,
  UNIQUE (name, team_id)
);

-- A worker's qualifications: one row per job they hold, with per-job hour
-- caps. Students cap at 20 hrs/wk across all jobs; staff per worker_type.
CREATE TABLE IF NOT EXISTS student_jobs (
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id    INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  min_hours INTEGER NOT NULL DEFAULT 0,
  max_hours INTEGER NOT NULL DEFAULT 20,
  active    BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (user_id, job_id)
);

-- All shifts. status: 'open' (available to pick) | 'assigned'.
-- parent_shift_id groups rows split from one original shift, so the calendar
-- can reconstruct the full shift and show which blocks are taken.
CREATE TABLE IF NOT EXISTS workqueue (
  id               SERIAL PRIMARY KEY,
  team_id          INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  job_id           INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
  parent_shift_id  INTEGER REFERENCES workqueue(id) ON DELETE SET NULL,
  date             DATE NOT NULL,
  start_time       TIME NOT NULL,
  end_time         TIME NOT NULL,
  status           TEXT NOT NULL DEFAULT 'open',
  assigned_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL
);

-- Worker requests. type: 'miss' (return an assigned shift to the workqueue)
-- | 'overflow' (pick a shift that exceeds max hours, needs manager override).
CREATE TABLE IF NOT EXISTS schedule_requests (
  id           SERIAL PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  workqueue_id INTEGER NOT NULL REFERENCES workqueue(id) ON DELETE CASCADE,
  type         TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'pending',
  reason       TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A job's daily operating hours (0=Sunday..6=Saturday); a day with no row
-- means the job is closed that day. Multiple shifts per day are allowed
-- (24h coverage = three 8h blocks), so uniqueness includes start_time.
CREATE TABLE IF NOT EXISTS job_schedules (
  id          SERIAL PRIMARY KEY,
  job_id      INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  day_of_week INTEGER NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL,
  UNIQUE (job_id, day_of_week, start_time)
);

-- Date-specific closures: a job closed on a particular calendar date,
-- overriding its weekly schedule (holidays, planned closings, etc.).
CREATE TABLE IF NOT EXISTS job_holidays (
  id     SERIAL PRIMARY KEY,
  job_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  date   DATE NOT NULL,
  reason TEXT,
  UNIQUE (job_id, date)
);

-- Audit trail for schedule actions: who did what to a request, shift, or job
-- assignment, and when. Actions are dotted lowercase past tense
-- (request.approved, shift.assigned, job.removed); details carries the
-- action-specific payload as JSON. Rows are never updated or deleted.
-- team_id scopes the row for the report (managers see their scope's rows);
-- NULL means unresolvable, which keeps the row admin-only.
CREATE TABLE IF NOT EXISTS audit_log (
  id          SERIAL PRIMARY KEY,
  actor_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  action      TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id   INTEGER,
  team_id     INTEGER REFERENCES teams(id) ON DELETE SET NULL,
  details     JSONB,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log (entity_type, entity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_actor ON audit_log (actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_team ON audit_log (team_id, created_at DESC);

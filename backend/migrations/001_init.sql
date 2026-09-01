-- Full schema for the student work-schedule system. Applied at boot; recorded
-- in schema_migrations.

CREATE TABLE IF NOT EXISTS users (
  id                  SERIAL PRIMARY KEY,
  email               TEXT NOT NULL UNIQUE,
  uid                 TEXT UNIQUE,
  password            TEXT NOT NULL,
  role                TEXT NOT NULL DEFAULT 'student',
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

-- ---- Student work-schedule system ----
-- The workqueue is the single source of truth for shifts; weekly_schedules is
-- a template the manager sets that generates the current week's assigned shifts.

CREATE TABLE IF NOT EXISTS locations (
  id           SERIAL PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  abbreviation TEXT,
  address      TEXT,
  address2     TEXT,
  city         TEXT,
  state        TEXT,
  zip          TEXT,
  country      TEXT
);

CREATE TABLE IF NOT EXISTS departments (
  id             SERIAL PRIMARY KEY,
  name           TEXT NOT NULL UNIQUE,
  department_code TEXT,
  location_id    INTEGER NOT NULL REFERENCES locations(id) ON DELETE CASCADE
);

-- A student's department + min/max weekly hours. One row per student.
CREATE TABLE IF NOT EXISTS student_assignments (
  user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
  min_hours     INTEGER NOT NULL DEFAULT 0,
  max_hours     INTEGER NOT NULL DEFAULT 20,
  active        BOOLEAN NOT NULL DEFAULT true
);

-- A manager's scope: everything in this location.
CREATE TABLE IF NOT EXISTS manager_assignments (
  user_id     INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  location_id INTEGER NOT NULL REFERENCES locations(id) ON DELETE CASCADE
);

-- Preset weekly template (0=Sunday..6=Saturday). Generates workqueue rows.
CREATE TABLE IF NOT EXISTS weekly_schedules (
  id          SERIAL PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day_of_week INTEGER NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL
);

-- Student's preferred days/times (informational, not enforced).
CREATE TABLE IF NOT EXISTS preferred_times (
  id          SERIAL PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day_of_week INTEGER NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL
);

-- All shifts. status: 'open' (available to pick) | 'assigned'.
CREATE TABLE IF NOT EXISTS workqueue (
  id              SERIAL PRIMARY KEY,
  department_id   INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
  date            DATE NOT NULL,
  start_time      TIME NOT NULL,
  end_time        TIME NOT NULL,
  status          TEXT NOT NULL DEFAULT 'open',
  assigned_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL
);

-- Student requests. type: 'miss' (return an assigned shift to the workqueue)
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

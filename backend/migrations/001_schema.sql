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


-- Job concept: a position a worker/student is qualified to do. A job belongs
-- to a department (the type of work); its location is the department's. A
-- student can hold multiple jobs (student_jobs), possibly in different
-- departments/locations. Replaces the single-department student_assignments.

CREATE TABLE IF NOT EXISTS jobs (
  id            SERIAL PRIMARY KEY,
  name          TEXT NOT NULL,
  department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
  UNIQUE (name, department_id)
);

-- A student's qualifications: one row per job they're assigned to.
CREATE TABLE IF NOT EXISTS student_jobs (
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  job_id    INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  min_hours INTEGER NOT NULL DEFAULT 0,
  max_hours INTEGER NOT NULL DEFAULT 20,
  active    BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (user_id, job_id)
);

-- Backfill: one default job per department (named after it), then migrate each
-- student's old single-department assignment into a student_jobs row.
INSERT INTO jobs (name, department_id)
SELECT name, id FROM departments;

INSERT INTO student_jobs (user_id, job_id, min_hours, max_hours, active)
SELECT sa.user_id, j.id, sa.min_hours, sa.max_hours, sa.active
FROM student_assignments sa
JOIN jobs j ON j.department_id = sa.department_id;

DROP TABLE student_assignments;


-- Per-job staffing requirements. A job's operating hours are defined per day of
-- week (0=Sunday..6=Saturday); a day with no row means the job is closed that
-- day (weekend, holiday, etc.). The daily hour requirement is the span between
-- start_time and end_time; the weekly requirement is the sum across days.

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS optimal_workers INTEGER NOT NULL DEFAULT 1;

-- A job's daily operating hours. One row per day the job is open.
CREATE TABLE IF NOT EXISTS job_schedules (
  id          SERIAL PRIMARY KEY,
  job_id      INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  day_of_week INTEGER NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
  start_time  TIME NOT NULL,
  end_time    TIME NOT NULL,
  UNIQUE (job_id, day_of_week)
);


-- Date-specific closures: a job closed on a particular calendar date, overriding
-- its weekly schedule (holidays, planned closings, etc.). The weekly hour
-- requirement is reduced by the hours of any holiday that falls on a day the
-- job would otherwise be open.

CREATE TABLE IF NOT EXISTS job_holidays (
  id     SERIAL PRIMARY KEY,
  job_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  date   DATE NOT NULL,
  reason TEXT,
  UNIQUE (job_id, date)
);


-- How a worker's weekly hours are governed. Anyone can be a worker: students
-- are capped at 20 hrs/wk across all jobs/departments; staff are classified
-- fulltime (40 regular + up to 20 overtime) or hourly (regular limit set by a
-- manager, default 40; overtime is anything over 40).
--
-- worker_type: 'student' | 'fulltime' | 'hourly' (default 'student').
-- hourly_limit: manager-set regular weekly hours for an 'hourly' worker
--   (1..40, where 40 is the default). NULL means use the 40 default.

ALTER TABLE users ADD COLUMN IF NOT EXISTS worker_type TEXT NOT NULL DEFAULT 'student';
ALTER TABLE users ADD COLUMN IF NOT EXISTS hourly_limit INTEGER;


-- Backfill default operating hours for jobs that have none: weekdays 9am-5pm
-- (8h each, 40h/wk) and weekends 10h each (20h/wk). Only touches jobs with no
-- existing job_schedules rows, so it's safe to re-run.
INSERT INTO job_schedules (job_id, day_of_week, start_time, end_time)
SELECT j.id, d.dow, d.start_time, d.end_time
FROM jobs j
CROSS JOIN (VALUES
  (1, '09:00'::time, '17:00'::time),
  (2, '09:00'::time, '17:00'::time),
  (3, '09:00'::time, '17:00'::time),
  (4, '09:00'::time, '17:00'::time),
  (5, '09:00'::time, '17:00'::time),
  (6, '10:00'::time, '20:00'::time),
  (0, '10:00'::time, '20:00'::time)
) AS d(dow, start_time, end_time)
WHERE NOT EXISTS (SELECT 1 FROM job_schedules js WHERE js.job_id = j.id);


-- Update weekday operating hours from the old 8am-4pm default to normal work
-- hours (9am-5pm). Only touches rows still on the old default, so manually
-- configured schedules are left alone. Idempotent.
UPDATE job_schedules
SET start_time = '09:00', end_time = '17:00'
WHERE day_of_week BETWEEN 1 AND 5
  AND start_time = '08:00' AND end_time = '16:00';


-- Scheduler-to-department scoping: a scheduler manages the schedule for one
-- department (and, by extension, its location).
CREATE TABLE IF NOT EXISTS scheduler_assignments (
  user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE
);


-- Allow a job to have multiple shifts per day. 24h security coverage needs
-- three 8h shifts (00:00-08:00, 08:00-16:00, 16:00-24:00) on every day, which
-- the old UNIQUE (job_id, day_of_week) constraint forbade.
ALTER TABLE job_schedules DROP CONSTRAINT job_schedules_job_id_day_of_week_key;
ALTER TABLE job_schedules ADD CONSTRAINT job_schedules_job_id_day_of_week_start_time_key
  UNIQUE (job_id, day_of_week, start_time);


-- Track which job a workqueue shift belongs to. The workqueue was department-
-- scoped, so a security shift and a maintenance shift in the same department
-- were indistinguishable and could be assigned to the wrong worker. With job_id
-- the seed (and schedulers) can match shifts to the workers qualified for them.
ALTER TABLE workqueue ADD COLUMN job_id INTEGER REFERENCES jobs(id) ON DELETE SET NULL;


-- Group workqueue rows that were split from one original shift. When a shift is
-- split into assigned blocks + open remainders, every row keeps a reference to
-- the original shift id so the scheduler calendar can reconstruct the full shift
-- and show which 2-hour slots are already taken.
ALTER TABLE workqueue ADD COLUMN parent_shift_id INTEGER REFERENCES workqueue(id) ON DELETE SET NULL;



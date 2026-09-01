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

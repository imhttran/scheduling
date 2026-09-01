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

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

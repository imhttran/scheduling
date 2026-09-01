-- Scheduler-to-department scoping: a scheduler manages the schedule for one
-- department (and, by extension, its location).
CREATE TABLE IF NOT EXISTS scheduler_assignments (
  user_id       INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE
);

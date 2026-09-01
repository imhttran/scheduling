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

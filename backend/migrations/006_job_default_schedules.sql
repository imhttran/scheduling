-- Backfill default operating hours for jobs that have none: weekdays 8h each
-- (40h/wk) and weekends 10h each (20h/wk). Only touches jobs with no existing
-- job_schedules rows, so it's safe to re-run.
INSERT INTO job_schedules (job_id, day_of_week, start_time, end_time)
SELECT j.id, d.dow, d.start_time, d.end_time
FROM jobs j
CROSS JOIN (VALUES
  (1, '08:00'::time, '16:00'::time),
  (2, '08:00'::time, '16:00'::time),
  (3, '08:00'::time, '16:00'::time),
  (4, '08:00'::time, '16:00'::time),
  (5, '08:00'::time, '16:00'::time),
  (6, '10:00'::time, '20:00'::time),
  (0, '10:00'::time, '20:00'::time)
) AS d(dow, start_time, end_time)
WHERE NOT EXISTS (SELECT 1 FROM job_schedules js WHERE js.job_id = j.id);

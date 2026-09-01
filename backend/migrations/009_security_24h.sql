-- Allow a job to have multiple shifts per day. 24h security coverage needs
-- three 8h shifts (00:00-08:00, 08:00-16:00, 16:00-24:00) on every day, which
-- the old UNIQUE (job_id, day_of_week) constraint forbade.
ALTER TABLE job_schedules DROP CONSTRAINT job_schedules_job_id_day_of_week_key;
ALTER TABLE job_schedules ADD CONSTRAINT job_schedules_job_id_day_of_week_start_time_key
  UNIQUE (job_id, day_of_week, start_time);

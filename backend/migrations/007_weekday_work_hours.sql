-- Update weekday operating hours from the old 8am-4pm default to normal work
-- hours (9am-5pm). Only touches rows still on the old default, so manually
-- configured schedules are left alone. Idempotent.
UPDATE job_schedules
SET start_time = '09:00', end_time = '17:00'
WHERE day_of_week BETWEEN 1 AND 5
  AND start_time = '08:00' AND end_time = '16:00';

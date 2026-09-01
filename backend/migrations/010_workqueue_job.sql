-- Track which job a workqueue shift belongs to. The workqueue was department-
-- scoped, so a security shift and a maintenance shift in the same department
-- were indistinguishable and could be assigned to the wrong worker. With job_id
-- the seed (and schedulers) can match shifts to the workers qualified for them.
ALTER TABLE workqueue ADD COLUMN job_id INTEGER REFERENCES jobs(id) ON DELETE SET NULL;

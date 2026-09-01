-- Group workqueue rows that were split from one original shift. When a shift is
-- split into assigned blocks + open remainders, every row keeps a reference to
-- the original shift id so the scheduler calendar can reconstruct the full shift
-- and show which 2-hour slots are already taken.
ALTER TABLE workqueue ADD COLUMN parent_shift_id INTEGER REFERENCES workqueue(id) ON DELETE SET NULL;

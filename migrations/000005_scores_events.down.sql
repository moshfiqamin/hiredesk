-- 000005_scores_events (down): reverses the up migration.
--
-- Neither table is referenced by anything in this migration, so the order
-- between them is free. Dropped in reverse creation order by convention.
DROP TABLE IF EXISTS application_events;
DROP TABLE IF EXISTS scores;

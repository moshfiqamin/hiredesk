-- 000006_review (down): reverses the up migration.
--
-- Neither table references the other, so the order between them is free.
-- Dropped in reverse creation order by convention.
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS interviewer_assignments;

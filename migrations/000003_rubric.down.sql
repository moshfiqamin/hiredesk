-- 000003_rubric (down): reverses the up migration.
--
-- One table, and nothing references it yet, so there is no ordering to
-- respect. The index goes with the table.
DROP TABLE IF EXISTS rubric_criteria;

-- 000012_search (down): removes full-text search.
--
-- Reverse order of the up migration. search_vector is generated from
-- search_document, so PostgreSQL refuses to drop search_document while
-- search_vector still exists. Dropping a column drops its indexes too; the
-- DROP INDEX is written out so the reversal reads as a mirror of the up.

DROP INDEX IF EXISTS applications_search_vector_idx;

ALTER TABLE applications DROP COLUMN IF EXISTS search_vector;

ALTER TABLE applications DROP COLUMN IF EXISTS search_document;

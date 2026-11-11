-- 000012_search (up): full-text search over what candidates wrote.
--
-- search_document is the searchable text of one application: its text and
-- choice answers, in form order, joined by newlines. search_vector is that
-- text parsed into lexemes, and the GIN index makes "which applications
-- contain these words" an index lookup rather than a scan.
--
-- The statements run in a deliberate order, not the order the columns are
-- listed: the backfill comes before the generated column and the index, so
-- the tsvector of each row is computed once and the GIN index is built in
-- bulk from final data. See docs/notes/08-1-search-schema.md.

-- A constant default makes this a catalogue-only change: no row is rewritten.
-- NOT NULL so that "no searchable text" has one spelling, the empty string.
ALTER TABLE applications
    ADD COLUMN search_document text NOT NULL DEFAULT '';

-- The same rule SubmitApplication applies in Go (domain.SearchDocument) and
-- the repair command applies in SQL (store.RebuildSearchDocuments):
--
--   - text and choice answers only. A number answer is "4" and a boolean is
--     "true", which as search terms match noise rather than meaning;
--   - in form order, so a document reads the way the form asked it;
--   - cut to 100,000 characters, which keeps the tsvector well under its
--     1 MB limit even for a document of nothing but distinct words.
--
-- Applications with no text answers match no row of the subquery and keep
-- the default ''.
UPDATE applications AS a
SET search_document = doc.text
FROM (
    SELECT aa.application_id,
           left(string_agg(aa.answer_text, E'\n' ORDER BY q.position), 100000) AS text
    FROM application_answers aa
    JOIN questions q ON q.id = aa.question_id
    WHERE q.kind IN ('text', 'choice')
    GROUP BY aa.application_id
) AS doc
WHERE a.id = doc.application_id;

-- Computed by PostgreSQL from search_document on every INSERT and on every
-- UPDATE that changes search_document, and impossible to write directly, so
-- it cannot disagree with the text it was built from.
--
-- 'english' must be spelled out. The one-argument to_tsvector reads the
-- default_text_search_config setting, so its result can change between
-- sessions; PostgreSQL refuses it in a generated column, which requires an
-- IMMUTABLE expression.
--
-- Adding a STORED generated column rewrites the table under an ACCESS
-- EXCLUSIVE lock. At this project's scale that is milliseconds.
ALTER TABLE applications
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (to_tsvector('english', search_document)) STORED;

-- GIN, an inverted index: one entry per lexeme, listing the rows that contain
-- it. That is the shape of the question @@ asks. A B-tree orders whole
-- values, and no ordering of whole documents answers "contains 'go'".
--
-- Not CONCURRENTLY: that cannot run inside the transaction this migration
-- runs in. A large production table would get the index in a migration of
-- its own, built CONCURRENTLY.
CREATE INDEX applications_search_vector_idx
    ON applications USING gin (search_vector);

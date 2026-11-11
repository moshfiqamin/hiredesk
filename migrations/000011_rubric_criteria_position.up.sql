-- 000011_rubric_criteria_position (up): the order of a rubric's criteria.
--
-- Until now a rubric was a set: the rows of one version share a created_at
-- (now() is the transaction's start time), so nothing recorded the order the
-- admin wrote them in, and a read sorted them by random UUID.
--
-- position also closes a race. Two rubric replacements that both read the
-- same highest version would both insert version N + 1, and the rubric would
-- become the union of two lists. The unique constraint below makes the second
-- transaction's first row collide with the first transaction's, so at most one
-- of them commits. The store also locks the post, so this is the backstop.

ALTER TABLE rubric_criteria ADD COLUMN position int;

-- Existing rows are numbered in the only stable order they have, so the
-- column can be made NOT NULL and the constraint added without a collision.
UPDATE rubric_criteria rc
SET position = numbered.position
FROM (
    SELECT id,
           row_number() OVER (PARTITION BY job_post_id, rubric_version ORDER BY created_at, id) AS position
    FROM rubric_criteria
) AS numbered
WHERE rc.id = numbered.id;

ALTER TABLE rubric_criteria ALTER COLUMN position SET NOT NULL;

ALTER TABLE rubric_criteria
    ADD CONSTRAINT rubric_criteria_position_check CHECK (position >= 1);

-- Its index also serves "every criterion of version N of this post, in
-- order", so the (job_post_id, rubric_version) index from 000003 is now a
-- prefix of this one and redundant.
ALTER TABLE rubric_criteria
    ADD CONSTRAINT rubric_criteria_job_post_id_rubric_version_position_key
    UNIQUE (job_post_id, rubric_version, position);

DROP INDEX rubric_criteria_job_post_id_rubric_version_idx;

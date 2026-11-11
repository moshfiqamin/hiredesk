-- 000008_questions_position_deferrable (down): restores the constraint as
-- 000002 created it, checked immediately and not deferrable.

ALTER TABLE questions
    DROP CONSTRAINT questions_job_post_id_position_key;

ALTER TABLE questions
    ADD CONSTRAINT questions_job_post_id_position_key
        UNIQUE (job_post_id, position);

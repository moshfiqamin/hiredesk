-- 000011_rubric_criteria_position (down): back to an unordered rubric.

CREATE INDEX rubric_criteria_job_post_id_rubric_version_idx
    ON rubric_criteria (job_post_id, rubric_version);

ALTER TABLE rubric_criteria
    DROP CONSTRAINT rubric_criteria_job_post_id_rubric_version_position_key;

ALTER TABLE rubric_criteria DROP COLUMN position;

-- 000002_job_posts (down): reverses the up migration.
--
-- questions holds the foreign key into job_posts, so it goes first. Each
-- DROP TABLE carries away its own indexes and constraints, so
-- questions_job_post_id_idx and questions_job_post_id_position_key need no
-- statement of their own.
DROP TABLE IF EXISTS questions;
DROP TABLE IF EXISTS job_posts;

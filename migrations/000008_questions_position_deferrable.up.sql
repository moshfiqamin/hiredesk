-- 000008_questions_position_deferrable (up): lets a reorder move questions
-- through each other's positions inside one transaction.
--
-- A plain UNIQUE constraint is checked row by row, the instant each row is
-- written. Reordering a form means some question must, for a moment, take a
-- position another question still holds, and that moment is refused. A
-- DEFERRABLE constraint can be told, per transaction, to wait until COMMIT
-- and check the finished result instead. 000002 declared this as a table
-- constraint, rather than a bare unique index, so that this was possible.
--
-- INITIALLY IMMEDIATE keeps today's behaviour for everything else: an INSERT
-- or UPDATE that collides is still refused at the end of its statement. Only
-- a transaction that runs SET CONSTRAINTS ... DEFERRED waits until COMMIT.
--
-- PostgreSQL's ALTER CONSTRAINT can change deferrability only for foreign
-- keys, so the constraint is dropped and added again. golang-migrate sends
-- this file as one multi-statement query, which PostgreSQL runs as a single
-- implicit transaction, so no other session sees the table without it.

ALTER TABLE questions
    DROP CONSTRAINT questions_job_post_id_position_key;

ALTER TABLE questions
    ADD CONSTRAINT questions_job_post_id_position_key
        UNIQUE (job_post_id, position)
        DEFERRABLE INITIALLY IMMEDIATE;

-- 000013_assignment_role_guard (down): back to the store as the only guard.
--
-- The foreign key depends on both the column and the unique constraint, so
-- it goes first.

ALTER TABLE interviewer_assignments
    DROP CONSTRAINT IF EXISTS interviewer_assignments_interviewer_is_interviewer_fkey;

ALTER TABLE interviewer_assignments DROP COLUMN IF EXISTS interviewer_role;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_id_role_key;

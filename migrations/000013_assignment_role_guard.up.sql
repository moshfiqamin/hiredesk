-- 000013_assignment_role_guard (up): only an interviewer can be assigned.
--
-- The store refuses to assign anyone whose role is not interviewer, with a
-- 422. This is the second line: the schema itself makes such a row
-- impossible, whichever code path writes it.
--
-- An ordinary foreign key only proves the user exists, and a CHECK can only
-- see the row being written, which has no role column. The rule is about a
-- property of the OTHER row. The way to make a foreign key check that
-- property is to copy it into this row, pin it with a CHECK, and reference
-- the pair:
--
--   interviewer_assignments (interviewer_id, interviewer_role = 'interviewer')
--       REFERENCES users (id, role)
--
-- The pair can only match a users row whose role really is 'interviewer'.
-- See docs/notes/09-1-assignment.md.
--
-- If any existing assignment names a non-interviewer, the ADD CONSTRAINT
-- below fails and the migration is rolled back. That is deliberate: such a
-- row is the escalation this guards against, and needs a person to look at
-- it, not a migration that quietly deletes it.

-- A foreign key may only reference columns covered by a unique constraint.
-- (id, role) is trivially unique, since id alone already is; the constraint
-- exists only so it can be referenced. It also makes role a "key" column for
-- row locking, so the FOR KEY SHARE lock a foreign-key check takes blocks a
-- concurrent role change.
ALTER TABLE users
    ADD CONSTRAINT users_id_role_key UNIQUE (id, role);

-- The default fills every existing row and every insert that does not name
-- the column, so no INSERT statement has to change. The CHECK stops anyone
-- writing a different value to sneak a candidate through the pair.
ALTER TABLE interviewer_assignments
    ADD COLUMN interviewer_role text NOT NULL DEFAULT 'interviewer'
        CONSTRAINT interviewer_assignments_interviewer_role_check
        CHECK (interviewer_role = 'interviewer');

-- ON DELETE CASCADE matches the existing single-column key on
-- interviewer_id. ON UPDATE is left at the default, NO ACTION, so changing an
-- assigned interviewer's role is refused until their assignments are
-- removed: a demoted user cannot keep read access to the applications they
-- were reviewing.
ALTER TABLE interviewer_assignments
    ADD CONSTRAINT interviewer_assignments_interviewer_is_interviewer_fkey
    FOREIGN KEY (interviewer_id, interviewer_role)
    REFERENCES users (id, role)
    ON DELETE CASCADE;

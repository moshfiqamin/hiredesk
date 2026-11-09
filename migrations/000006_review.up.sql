-- 000006_review (up): who is reviewing an application, and what mark they gave.
--
-- Two tables rather than one, because an assignment and a review have
-- different lifetimes: an interviewer is assigned before they have an opinion,
-- and may never file one. Folding them together would mean a nullable mark
-- that cannot distinguish "not reviewed yet" from "reviewed, no score".
--
-- Depends on 000001 (users) and 000004 (applications).

CREATE TABLE interviewer_assignments (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,

    -- CASCADE, per the brief: an assignment is a work item, not a record. If
    -- the interviewer's account goes, the task to review goes with it.
    interviewer_id uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- The recruiter who made the assignment. SET NULL so the assignment
    -- survives their account; nullable for the same reason.
    assigned_by    uuid        REFERENCES users (id) ON DELETE SET NULL,

    created_at     timestamptz NOT NULL DEFAULT now(),

    -- One assignment per interviewer per application, so a double-assign is
    -- idempotent rather than two identical items in their queue.
    CONSTRAINT interviewer_assignments_application_id_interviewer_id_key
        UNIQUE (application_id, interviewer_id)
);

-- "Everything assigned to me", the interviewer's queue. Also the index the
-- ON DELETE CASCADE from users needs, since interviewer_id is the second
-- column of the unique constraint and no index leads with it.
CREATE INDEX interviewer_assignments_interviewer_id_idx
    ON interviewer_assignments (interviewer_id);

CREATE TABLE reviews (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,

    -- RESTRICT, unlike the assignment above: a review is the evidence behind a
    -- hiring decision and must say who gave the mark. NOT NULL for the same
    -- reason, and because a NULL here would punch a hole in the unique
    -- constraint below, where NULLs do not collide.
    --
    -- The consequence is deliberate: a user who has filed a review cannot be
    -- deleted. Retire such an account by anonymising the row, not removing it.
    interviewer_id uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,

    -- smallint is ample for 1-10 and documents the range at the type level.
    mark           smallint    NOT NULL,

    -- Nullable: a mark without commentary is a complete review.
    notes          text,

    created_at     timestamptz NOT NULL DEFAULT now(),

    -- Present here, unlike application_events: a review is an opinion its
    -- author may legitimately revise, so this table is mutable by design.
    updated_at     timestamptz NOT NULL DEFAULT now(),

    -- BETWEEN is inclusive, so 1 and 10 are both valid marks.
    CONSTRAINT reviews_mark_check
        CHECK (mark BETWEEN 1 AND 10),

    -- One review per interviewer per application. A second opinion from the
    -- same person is an edit of their review, not another row.
    CONSTRAINT reviews_application_id_interviewer_id_key
        UNIQUE (application_id, interviewer_id)
);

-- "Every review I have filed", and the index the RESTRICT check above needs to
-- answer "has this user reviewed anything?" without scanning the table.
CREATE INDEX reviews_interviewer_id_idx ON reviews (interviewer_id);

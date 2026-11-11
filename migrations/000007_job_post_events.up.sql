-- 000007_job_post_events (up): the audit trail of a job post.
--
-- A sibling of application_events rather than a reuse of it. That table's
-- application_id is NOT NULL and references applications, so it cannot hold
-- an event about a post; widening it would mean a nullable application_id, a
-- second nullable job_post_id, and a CHECK that exactly one is set, with every
-- reader branching on which.
--
-- Depends on 000001 (users) and 000002 (job_posts).

CREATE TABLE job_post_events (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- CASCADE, matching application_events: the trail is part of the post.
    -- Posts are closed, not deleted, so in practice it is never exercised.
    job_post_id uuid        NOT NULL REFERENCES job_posts (id) ON DELETE CASCADE,

    -- Who caused it. NULL means the system, with no human actor. SET NULL so
    -- the trail survives the actor's account being deleted.
    actor_id    uuid        REFERENCES users (id) ON DELETE SET NULL,

    -- Not fenced by a CHECK, as in application_events: an append-only log
    -- should never refuse a record of something that did happen.
    type        text        NOT NULL,

    -- Per-type detail, e.g. {"from":"draft","to":"open"}.
    payload     jsonb       NOT NULL DEFAULT '{}'::jsonb,

    created_at  timestamptz NOT NULL DEFAULT now()

    -- No updated_at: like application_events, this table is append-only, and
    -- a correction is a new event.
);

-- "The history of this post, newest first", the only way it is read.
CREATE INDEX job_post_events_job_post_id_created_at_idx
    ON job_post_events (job_post_id, created_at DESC);

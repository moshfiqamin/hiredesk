-- 000005_scores_events (up): the output of scoring an application, and the
-- audit trail of everything that has happened to it.
--
-- Depends on 000001, 000003 (rubric_criteria) and 000004.

CREATE TABLE scores (
    id                 uuid          PRIMARY KEY DEFAULT gen_random_uuid(),

    application_id     uuid          NOT NULL REFERENCES applications (id) ON DELETE CASCADE,

    -- RESTRICT for the same reason as application_answers.question_id: a score
    -- whose criterion has been deleted cannot be explained to a candidate who
    -- asks why they were rejected.
    criterion_id       uuid          NOT NULL REFERENCES rubric_criteria (id) ON DELETE RESTRICT,

    -- Points earned out of points available, rather than a percentage, so the
    -- weighted total is recomputed without re-reading the rubric. numeric, not
    -- float, so a column of scores sums to exactly the displayed total.
    earned             numeric(6, 2) NOT NULL,
    available          numeric(6, 2) NOT NULL,

    -- Why the scorer reached this number, in words, shown on the scorecard.
    -- NOT NULL: a score nobody can explain is not defensible to a candidate.
    detail             text          NOT NULL,

    -- The one answer this score is based on, when there is one. NULL means the
    -- criterion did not score a single answer — a keyword match reads the CV,
    -- and boolean_all spans several questions. SET NULL rather than RESTRICT
    -- because the evidence link is an optional convenience, not the score.
    evidence_answer_id uuid          REFERENCES application_answers (id) ON DELETE SET NULL,

    -- Copied, not joined: which version of the rubric produced this number.
    -- A score must stay interpretable after the rubric is revised.
    rubric_version     int           NOT NULL,

    computed_at        timestamptz   NOT NULL DEFAULT now()
);

-- "Every score for this application", which is the scorecard query.
CREATE INDEX scores_application_id_idx ON scores (application_id);

CREATE TABLE application_events (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,

    -- Who caused it. NULL means the system did, with no human actor — an
    -- automated rescore, a scheduled rejection. SET NULL so the trail survives
    -- the actor's account being deleted; the event still happened.
    actor_id       uuid        REFERENCES users (id) ON DELETE SET NULL,

    -- Deliberately not fenced by a CHECK, unlike every other type column here.
    -- New event types are added by the code that emits them, and an append-only
    -- log should never reject a record of something that did happen.
    type           text        NOT NULL,

    -- Per-type detail, e.g. {"from":"submitted","to":"shortlisted"}. jsonb for
    -- the same reason as rubric_criteria.config: the shape varies by type.
    payload        jsonb       NOT NULL DEFAULT '{}'::jsonb,

    created_at     timestamptz NOT NULL DEFAULT now()

    -- No updated_at, and no status column, on purpose. This table is
    -- append-only: an event is a claim that something happened at an instant,
    -- so editing one would be falsifying history rather than correcting data.
    -- A correction is a new event. The absence of the column is the signal;
    -- see the note for the trigger that enforces it.
);

-- Composite, because the query is always "the latest N events for this
-- application": ORDER BY created_at DESC LIMIT 20. An index on application_id
-- alone finds every event for the application and then sorts them; this one
-- stores them in order, so the scan stops after N rows.
--
-- The DESC is near-cosmetic: a btree is bidirectional, so an ASC index serves
-- the same query via an Index Scan Backward. It is written DESC to match the
-- only ordering this table is read in. Direction would genuinely matter only
-- for a mixed ORDER BY, such as (application_id, created_at DESC, id ASC).
CREATE INDEX application_events_application_id_created_at_idx
    ON application_events (application_id, created_at DESC);

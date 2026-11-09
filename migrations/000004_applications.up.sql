-- 000004_applications (up): a candidate's application to a post, and the
-- answers they gave to its questions.
--
-- Depends on 000001 (users) and 000002 (job_posts, questions).
--
-- Delete policy across this migration and the next two: a row that is PART OF
-- an application cascades with it; a row that merely REFERS to a person is set
-- to NULL so the record outlives the account; a row that is EVIDENCE refuses
-- to let its definition be deleted underneath it.

CREATE TABLE applications (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Both NOT NULL beyond the brief: an application with no post or no
    -- candidate is not a partial record, it is an uninterpretable one.
    job_post_id   uuid        NOT NULL REFERENCES job_posts (id) ON DELETE CASCADE,
    candidate_id  uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    status        text        NOT NULL DEFAULT 'submitted',

    -- A link to object storage, not the file. Keeping binaries out of the
    -- database keeps backups small and lets the CDN serve the download.
    cv_url        text        NOT NULL,

    submitted_at  timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT applications_status_check
        CHECK (status IN ('submitted', 'shortlisted', 'interview', 'offer', 'rejected')),

    -- The duplicate-application rule, and the only enforcement of it that
    -- survives concurrency. A SELECT-then-INSERT check in the handler has a
    -- gap between the two statements in which a second request sees the same
    -- empty result; this constraint is evaluated inside the index insertion,
    -- so of two simultaneous inserts exactly one survives.
    --
    -- Go side: INSERT ... ON CONFLICT DO NOTHING, or map SQLSTATE 23505 to a
    -- 409 Conflict. Never a pre-flight SELECT.
    CONSTRAINT applications_job_post_id_candidate_id_key
        UNIQUE (job_post_id, candidate_id)
);

-- The recruiter's pipeline view: "every shortlisted application on this post".
-- Not redundant against the unique constraint above — that index's second
-- column is candidate_id, so it cannot filter on status.
CREATE INDEX applications_job_post_id_status_idx
    ON applications (job_post_id, status);

-- "Every application by this candidate", for their dashboard. Also the index
-- the ON DELETE CASCADE from users needs: candidate_id is the second column of
-- the unique constraint, so no existing index leads with it.
CREATE INDEX applications_candidate_id_idx ON applications (candidate_id);

CREATE TABLE application_answers (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Part of the application, so it cascades with it.
    application_id uuid        NOT NULL REFERENCES applications (id) ON DELETE CASCADE,

    -- RESTRICT, not CASCADE: this row is the evidence of what the candidate
    -- was asked and what they said. Deleting the question would make the
    -- answer uninterpretable, so the database refuses instead. The practical
    -- effect is that a post's form freezes once anyone has applied, which is
    -- the correct business rule.
    question_id    uuid        NOT NULL REFERENCES questions (id) ON DELETE RESTRICT,

    -- One column for every kind of answer: 'number' and 'boolean' answers are
    -- stored as their text form and parsed against questions.kind. A column
    -- per type would be four nullable columns and the same parsing anyway.
    answer_text    text        NOT NULL,

    created_at     timestamptz NOT NULL DEFAULT now(),

    -- One answer per question per application. Without this, a retried submit
    -- stores the same answer twice and the scorer counts it twice.
    CONSTRAINT application_answers_application_id_question_id_key
        UNIQUE (application_id, question_id)
);

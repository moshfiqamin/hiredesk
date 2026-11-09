-- 000002_job_posts (up): the vacancy itself, and the application form attached
-- to it.
--
-- Depends on 000001: job_posts.created_by points at users.

CREATE TABLE job_posts (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    title           text        NOT NULL,

    -- Nullable: a draft is created from a title alone and filled in later.
    description     text,
    employment_type text,
    location        text,

    -- A post always hires at least one person, so zero or negative is a bug
    -- rather than a state to represent. int, not smallint: the two-byte saving
    -- is irrelevant and widening a column later needs a table rewrite.
    vacancies       int         NOT NULL DEFAULT 1,

    -- numeric, never float: exact decimal arithmetic, so a salary read back is
    -- the salary written. (12,2) is 10 digits before the point and 2 after.
    -- Nullable because "salary negotiable" is a real posting.
    salary_min      numeric(12, 2),
    salary_max      numeric(12, 2),

    -- Starts as a draft, so an accidental INSERT is never a live vacancy.
    status          text        NOT NULL DEFAULT 'draft',

    -- Nullable: a post can stay open until filled, with no deadline.
    closes_at       timestamptz,

    -- ON DELETE SET NULL, unlike the CASCADE in 000001: a job post is a
    -- business record that must outlive the recruiter's account, so deleting
    -- the user forgets the author rather than destroying the post. This is why
    -- the column is nullable. RESTRICT was rejected because it would block
    -- deleting any user who had ever posted a job.
    created_by      uuid        REFERENCES users (id) ON DELETE SET NULL,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT job_posts_vacancies_check
        CHECK (vacancies > 0),

    CONSTRAINT job_posts_status_check
        CHECK (status IN ('draft', 'open', 'closed'))
);

CREATE TABLE questions (
    id          uuid    PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NOT NULL beyond the brief: a question that belongs to no post could
    -- never be rendered on a form. CASCADE because the question is part of
    -- the post, not a record with its own standing.
    job_post_id uuid    NOT NULL REFERENCES job_posts (id) ON DELETE CASCADE,

    prompt      text    NOT NULL,

    -- Decides how the answer is validated and scored, and which of the
    -- columns below apply: 'choice' needs choices, 'text' needs min_length.
    kind        text    NOT NULL,

    -- A Postgres array column. Correct here because the options are an
    -- ordered, display-only list owned entirely by this row and never queried
    -- on their own. A child table would be needed the moment an option
    -- requires its own id, label or foreign key.
    choices     text[],

    required    boolean NOT NULL DEFAULT true,

    -- Minimum characters for a free-text answer. 0 means "no minimum", which
    -- keeps the column NOT NULL and spares every reader a null check.
    min_length  int     NOT NULL DEFAULT 0,

    -- Display order within the post. Not a reserved word in Postgres, so it
    -- needs no quoting, despite POSITION() being a built-in function.
    position    int     NOT NULL,

    CONSTRAINT questions_kind_check
        CHECK (kind IN ('text', 'number', 'boolean', 'choice')),

    -- Two questions on one post cannot claim the same slot, so ORDER BY
    -- position is a total order and the form renders identically every time.
    -- Scoped to the post, so each post numbers its own questions from 1.
    --
    -- Declared as a table constraint rather than a bare unique index so it can
    -- later be made DEFERRABLE, which bulk reordering needs.
    CONSTRAINT questions_job_post_id_position_key
        UNIQUE (job_post_id, position)
);

-- Requested in the brief, and kept for that reason. Note that it is covered by
-- the leading column of questions_job_post_id_position_key: that index already
-- serves both "all questions for this post" and the ON DELETE CASCADE check,
-- so this one earns its write cost only for pure equality lookups.
CREATE INDEX questions_job_post_id_idx ON questions (job_post_id);

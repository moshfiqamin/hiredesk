-- 000003_rubric (up): the scoring rubric for a job post.
--
-- One row is one scoring criterion. A post's rubric is every row sharing its
-- job_post_id and rubric_version.
--
-- Depends on 000002: job_post_id and question_id point into that migration.

CREATE TABLE rubric_criteria (
    id             uuid          PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NOT NULL beyond the brief: a criterion scores one post's applications,
    -- so it cannot stand alone. CASCADE because the rubric is part of the
    -- post rather than an independent record.
    job_post_id    uuid          NOT NULL REFERENCES job_posts (id) ON DELETE CASCADE,

    -- Shown to the reviewer on the scorecard, e.g. "Go experience".
    label          text          NOT NULL,

    -- Selects the scoring algorithm, and with it the shape of config below.
    kind           text          NOT NULL,

    -- Relative importance in the weighted total. numeric, not float, so the
    -- weights of a rubric sum to exactly what the recruiter typed. (6,2)
    -- allows 9999.99, far beyond any sane weight.
    weight         numeric(6, 2) NOT NULL,

    -- Per-kind parameters, because each kind takes different ones:
    -- keyword_match wants a keyword list, experience_years a threshold. jsonb
    -- rather than one nullable column per parameter, so adding a kind needs
    -- no DDL. The cost is that the database cannot validate the shape — that
    -- check belongs in Go, at the API boundary.
    --
    -- Defaulted to an empty object, not NULL, so readers never branch on two
    -- kinds of emptiness.
    config         jsonb         NOT NULL DEFAULT '{}'::jsonb,

    -- Set when the criterion scores one specific answer; NULL when it scores
    -- the application as a whole. CASCADE, not SET NULL: a criterion whose
    -- question is gone must disappear, because NULL already means something
    -- else here, and silently converting it would turn "depth of the answer
    -- to Q3" into a whole-application criterion.
    question_id    uuid          REFERENCES questions (id) ON DELETE CASCADE,

    -- Rubrics are versioned rather than edited, so applications already
    -- scored keep the rubric they were judged against.
    rubric_version int           NOT NULL DEFAULT 1,

    created_at     timestamptz   NOT NULL DEFAULT now(),

    CONSTRAINT rubric_criteria_kind_check
        CHECK (kind IN ('keyword_match', 'experience_years', 'boolean_all', 'answer_depth')),

    -- A negative weight would subtract from the total, which no criterion
    -- should do. Zero is allowed: it parks a criterion without deleting it.
    CONSTRAINT rubric_criteria_weight_check
        CHECK (weight >= 0)
);

-- Matches the only hot query: "every criterion in version N of this post's
-- rubric". Composite and in this order because job_post_id is always an
-- equality match, so it belongs first; the index then also serves a lookup on
-- job_post_id alone, and covers the ON DELETE CASCADE check from job_posts.
CREATE INDEX rubric_criteria_job_post_id_rubric_version_idx
    ON rubric_criteria (job_post_id, rubric_version);

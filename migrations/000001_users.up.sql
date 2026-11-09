-- 000001_users (up): identity tables.
--
-- Covers people (users) and the long-lived half of the session mechanism
-- (refresh_tokens). Access tokens are stateless JWTs and are not stored.

-- gen_random_uuid() lives in core Postgres from version 13 on, so this is
-- redundant on the postgres:16 image the stack runs. Kept so the migration
-- also applies cleanly on an older server, and because later work (digest(),
-- crypt()) will want pgcrypto present anyway.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    -- The database mints the id, so an INSERT need not supply one and the
    -- value is known to be unique without a round trip to a sequence.
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Stored verbatim, as the user typed it: "Rahim.Khan@Example.com" keeps
    -- its capitals for display. Case-insensitive uniqueness is enforced by
    -- users_email_lower_idx below, not by this column.
    email         text        NOT NULL,

    -- A bcrypt/argon2 digest, never a password. The column name is the
    -- contract: anything that puts a plaintext password here is a bug.
    password_hash text        NOT NULL,

    full_name     text        NOT NULL,

    -- Free-text column fenced in by a CHECK rather than a Postgres ENUM type,
    -- so adding a role later is an ordinary ALTER instead of a type migration.
    role          text        NOT NULL,

    -- timestamptz, not timestamp: stored as an absolute instant in UTC, so
    -- users in Dhaka, London and Sydney all read back the same moment.
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    -- Named, so a violation surfaces as "users_role_check" rather than an
    -- anonymous constraint name the API cannot map to a useful message.
    CONSTRAINT users_role_check
        CHECK (role IN ('admin', 'interviewer', 'candidate'))
);

-- Uniqueness on the folded value: one account per address, whatever case it
-- was registered in. A plain UNIQUE on email would let Rahim@x.com and
-- rahim@x.com both exist. Being an index on the same expression, it is also
-- what makes the login lookup `WHERE lower(email) = lower($1)` a seek rather
-- than a full scan.
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NOT NULL beyond the brief: an ownerless refresh token could never be
    -- matched to a caller, so the row would be unusable rather than merely
    -- incomplete. ON DELETE CASCADE makes deleting a user delete their
    -- tokens in the same statement, so no row can outlive its owner.
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- SHA-256 of the token, not the token. A stolen database dump then yields
    -- nothing a thief can present to the API. UNIQUE both enforces that one
    -- token belongs to one row and provides the index the lookup-on-refresh
    -- path needs.
    token_hash text        NOT NULL UNIQUE,

    -- No default: expiry is the token's lifetime, which the application
    -- decides (now() + 30 days, say), not a property of the table.
    expires_at timestamptz NOT NULL,

    -- Nullable on purpose — NULL means "still valid". Revocation sets the
    -- instant, which keeps the audit trail a boolean flag would discard.
    revoked_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now()
);

-- "All tokens for this user", used when revoking every session on password
-- change or sign-out-everywhere. The foreign key constrains values but does
-- not index them, so without this each such query scans the whole table —
-- and so does the cascade check when a user is deleted.
CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);

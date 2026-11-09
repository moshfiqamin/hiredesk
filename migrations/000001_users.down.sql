-- 000001_users (down): reverses the up migration.
--
-- Reverse order of creation: refresh_tokens holds the foreign key, so it goes
-- first. Dropping users first would fail on the dependency.
--
-- Each DROP TABLE takes its own indexes and constraints with it, so
-- users_email_lower_idx and refresh_tokens_user_id_idx need no separate
-- statement.
--
-- IF EXISTS keeps the migration idempotent: a `migrate down` after a failed,
-- partially applied `up` still completes instead of erroring on a table that
-- was never created.
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;

-- pgcrypto is deliberately left installed. An extension is database-wide, not
-- owned by this migration, so dropping it here would pull the rug from under
-- anything else in the database that uses it.

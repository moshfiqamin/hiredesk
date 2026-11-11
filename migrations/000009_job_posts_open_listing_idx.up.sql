-- 000009_job_posts_open_listing_idx (up): the index the public job listing
-- reads.
--
-- The listing asks for open posts, newest first, starting after a given
-- (created_at, id). With this index PostgreSQL descends the tree straight to
-- that point and reads forward exactly one page; it never visits the rows
-- before it, however deep the client has paged.
--
-- Partial (WHERE status = 'open'), because the listing never wants a draft or
-- a closed post. The index then holds only the rows it serves, and a post
-- enters and leaves it as it opens and closes.
--
-- id is in the key, not only the ORDER BY, so that rows with the same
-- created_at have a defined order the cursor can resume from.

CREATE INDEX job_posts_open_created_at_id_idx
    ON job_posts (created_at DESC, id DESC)
    WHERE status = 'open';

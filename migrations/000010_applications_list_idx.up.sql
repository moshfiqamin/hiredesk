-- 000010_applications_list_idx (up): the index the applications list reads
-- when it is not narrowed to one candidate or one post.
--
-- GET /v1/applications pages newest first, starting after a given
-- (submitted_at, id). For an admin with no filter, no existing index leads
-- with submitted_at, so every page would read and sort the whole table. With
-- this one PostgreSQL starts at the cursor and reads one page.
--
-- Not partial: unlike the public listing, every status is listed.

CREATE INDEX applications_submitted_at_id_idx
    ON applications (submitted_at DESC, id DESC);

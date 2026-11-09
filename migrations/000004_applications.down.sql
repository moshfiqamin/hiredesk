-- 000004_applications (down): reverses the up migration.
--
-- application_answers references applications, so it goes first.
DROP TABLE IF EXISTS application_answers;
DROP TABLE IF EXISTS applications;

-- Postgres's default text-search parser treats a filename like
-- "photo-alpha.png" as a single opaque token (it pattern-matches
-- dotted/hyphenated strings as "file" tokens), so plainto_tsquery('photo')
-- never matched it — a real bug found by actually running search, not a
-- theoretical concern. Fix: strip name to alphanumeric-plus-space before
-- tokenizing, so "photo-alpha.png" becomes tokens {photo, alpha, png}.
DROP INDEX IF EXISTS idx_files_search;
ALTER TABLE files DROP COLUMN name_tsv;
ALTER TABLE files ADD COLUMN name_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', regexp_replace(name, '[^a-zA-Z0-9]+', ' ', 'g'))) STORED;
CREATE INDEX idx_files_search ON files USING GIN (name_tsv);

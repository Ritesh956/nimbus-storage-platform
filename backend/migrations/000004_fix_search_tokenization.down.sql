DROP INDEX IF EXISTS idx_files_search;
ALTER TABLE files DROP COLUMN name_tsv;
ALTER TABLE files ADD COLUMN name_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', name)) STORED;
CREATE INDEX idx_files_search ON files USING GIN (name_tsv);

DROP INDEX idx_chunks_doomed;
DROP INDEX idx_file_version_chunks_hash;
ALTER TABLE chunks DROP COLUMN gc_state, DROP COLUMN last_seen_at, DROP COLUMN doomed_at;
DROP TYPE chunk_gc_state;

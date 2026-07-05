-- Chunk garbage collection (post-v1 backlog #10, docs/07-distributed-architecture.md §6).
-- Dedup means deleting a file never frees storage: purged files leave
-- unreferenced chunks rows + MinIO objects behind forever. GC state lives on
-- the chunks row itself:
--   gc_state     'live' | 'doomed'. Doomed chunks are invisible to the dedup
--                check (treated as missing, so clients re-upload the bytes)
--                and are physically reaped by nimbus-worker's sweeper after a
--                second grace window.
--   last_seen_at the dedup-lease clock: touched whenever a chunk is reported
--                "already stored" to a client (POST /v1/chunks/check and the
--                /complete validation) — a chunk is only doomed once it has
--                been both unreferenced AND unseen for the grace window.
--   doomed_at    when the mark phase doomed it; the sweep phase only deletes
--                after a further grace window, and a re-upload commit resets
--                gc_state to 'live' (resurrection).

CREATE TYPE chunk_gc_state AS ENUM ('live', 'doomed');

ALTER TABLE chunks
    ADD COLUMN gc_state     chunk_gc_state NOT NULL DEFAULT 'live',
    ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN doomed_at    timestamptz;

-- The refcount is computed, not stored: "is this chunk referenced?" is a
-- NOT EXISTS probe against file_version_chunks, which needs an index on
-- chunk_hash (the PK is (version_id, sequence), useless for that direction).
CREATE INDEX idx_file_version_chunks_hash ON file_version_chunks (chunk_hash);

-- Sweep-phase candidate scan: doomed chunks past their deletion window.
CREATE INDEX idx_chunks_doomed ON chunks (doomed_at) WHERE gc_state = 'doomed';

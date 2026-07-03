-- Upload-session bookkeeping. Not in the original docs/05-database-design.md
-- schema (Phase 6 didn't anticipate needing ephemeral chunk-attempt state)
-- — added here, doc updated to match, per the project's own "flag
-- divergences" rule rather than silently patching the schema.

CREATE TYPE upload_status AS ENUM ('in_progress', 'completed', 'aborted');

CREATE TABLE uploads (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    folder_id           uuid NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    name                text NOT NULL,
    declared_size_bytes bigint NOT NULL,
    mime_type           text NOT NULL,
    created_by          uuid NOT NULL REFERENCES users(id),
    status              upload_status NOT NULL DEFAULT 'in_progress',
    idempotency_key     text,
    file_id             uuid REFERENCES files(id),
    version_id          uuid REFERENCES file_versions(id),
    created_at          timestamptz NOT NULL DEFAULT now()
);
-- Idempotency is scoped per-user: two different users are never meant to
-- collide on the same client-generated key.
CREATE UNIQUE INDEX uq_uploads_idempotency_key ON uploads (created_by, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TYPE chunk_attempt_state AS ENUM ('pending', 'presigned', 'committed');

CREATE TABLE upload_chunks (
    upload_id  uuid NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    chunk_hash char(64) NOT NULL,
    state      chunk_attempt_state NOT NULL DEFAULT 'pending',
    etags      jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (upload_id, chunk_hash)
);

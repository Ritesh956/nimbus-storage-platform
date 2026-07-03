-- Initial schema, per docs/05-database-design.md.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         citext UNIQUE NOT NULL,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organizations (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL,
    owner_user_id uuid NOT NULL REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE member_role AS ENUM ('owner', 'member');

CREATE TABLE memberships (
    org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       member_role NOT NULL DEFAULT 'member',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

CREATE TABLE folders (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    parent_id  uuid REFERENCES folders(id) ON DELETE CASCADE,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX idx_folders_parent ON folders (org_id, parent_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_folders_sibling_name ON folders (org_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'), lower(name))
    WHERE deleted_at IS NULL;

CREATE TABLE files (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    folder_id          uuid NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    name               text NOT NULL,
    latest_version_id  uuid,
    created_by         uuid NOT NULL REFERENCES users(id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz,
    name_tsv           tsvector GENERATED ALWAYS AS (to_tsvector('simple', name)) STORED
);
CREATE INDEX idx_files_folder ON files (folder_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_files_search ON files USING GIN (name_tsv);
CREATE INDEX idx_files_org_deleted ON files (org_id) WHERE deleted_at IS NOT NULL;

CREATE TABLE file_versions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id         uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    size_bytes      bigint NOT NULL,
    checksum_sha256 char(64) NOT NULL,
    mime_type       text NOT NULL,
    thumbnail_key   text,
    created_by      uuid NOT NULL REFERENCES users(id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_file_versions_file ON file_versions (file_id, created_at DESC);

ALTER TABLE files ADD CONSTRAINT fk_files_latest_version
    FOREIGN KEY (latest_version_id) REFERENCES file_versions(id);

CREATE TABLE chunks (
    hash       char(64) PRIMARY KEY,
    size_bytes int NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE file_version_chunks (
    version_id uuid NOT NULL REFERENCES file_versions(id) ON DELETE CASCADE,
    sequence   int NOT NULL,
    chunk_hash char(64) NOT NULL REFERENCES chunks(hash),
    PRIMARY KEY (version_id, sequence)
);

CREATE TYPE node_status AS ENUM ('healthy', 'down');

CREATE TABLE storage_nodes (
    id                text PRIMARY KEY,
    endpoint          text NOT NULL,
    status            node_status NOT NULL DEFAULT 'healthy',
    last_heartbeat_at timestamptz
);

CREATE TYPE chunk_location_status AS ENUM ('committed', 'degraded');

CREATE TABLE chunk_locations (
    chunk_hash char(64) NOT NULL REFERENCES chunks(hash) ON DELETE CASCADE,
    node_id    text NOT NULL REFERENCES storage_nodes(id),
    status     chunk_location_status NOT NULL DEFAULT 'committed',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chunk_hash, node_id)
);
CREATE INDEX idx_chunk_locations_node ON chunk_locations (node_id);

CREATE TABLE share_links (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id    uuid NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    token      char(43) UNIQUE NOT NULL,
    created_by uuid NOT NULL REFERENCES users(id),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE activity_events (
    id            bigserial PRIMARY KEY,
    org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES users(id),
    verb          text NOT NULL,
    target_type   text NOT NULL,
    target_id     uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_activity_org_time ON activity_events (org_id, created_at DESC);

CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id  uuid NOT NULL,
    token_hash char(64) UNIQUE NOT NULL,
    used_at    timestamptz,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_family ON refresh_tokens (family_id);

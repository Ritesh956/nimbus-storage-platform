-- Per-org proof-of-possession for chunk dedup (audit §05). Global chunk
-- dedup is intentional and stays global (FR-7, no proof-of-possession check
-- at the storage layer) — but before this, nothing distinguished "this
-- content exists somewhere in the cluster" from "this org actually PUT and
-- committed these exact bytes", so a hash learned out-of-band (a leaked log
-- line, say) was enough to attach to another org's content at /complete
-- without ever uploading it. This table records the latter per org; see
-- upload.Repository.RecordOrgChunkProof / FindMissingChunksForOrg.
CREATE TABLE org_chunk_proofs (
    org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    chunk_hash char(64) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, chunk_hash)
);

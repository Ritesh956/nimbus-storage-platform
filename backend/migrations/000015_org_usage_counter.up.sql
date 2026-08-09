-- Audit §06: OrgUsageBytes did a live SUM join across file_versions/files on
-- every quota check (upload init and complete) — correct but a full-table
-- scan per check. Replace it with a counter maintained inside the same
-- transaction as every write that changes an org's stored bytes (version
-- insert, file purge, folder-cascade purge), so reads become O(1).
ALTER TABLE organizations ADD COLUMN usage_bytes bigint NOT NULL DEFAULT 0
    CONSTRAINT chk_orgs_usage_bytes_nonnegative CHECK (usage_bytes >= 0);

-- Backfill from the real data — same aggregation OrgUsageBytes used to run
-- per-request, run once here instead.
UPDATE organizations o SET usage_bytes = COALESCE((
    SELECT SUM(v.size_bytes)
    FROM file_versions v JOIN files f ON f.id = v.file_id
    WHERE f.org_id = o.id
), 0);

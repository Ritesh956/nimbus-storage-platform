-- Audit §06: org storage quota was a single NIMBUS_ORG_QUOTA_BYTES config
-- value applied identically to every org, with no per-tenant override in
-- the data model at all — a real production system needs to differentiate
-- tenants (a paid org vs. a free one), not just a single global number.
-- NULL means "no override, use the configured default" — the common case.
ALTER TABLE organizations ADD COLUMN quota_bytes_override bigint
    CONSTRAINT chk_orgs_quota_override_positive CHECK (quota_bytes_override IS NULL OR quota_bytes_override > 0);

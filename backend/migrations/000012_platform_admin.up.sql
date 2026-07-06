-- Platform-admin flag (post-Tier-4 governance session): gates the
-- /v1/admin/* cluster-ops routes (node health, hash ring, DLQ), which were
-- previously open to any authenticated user. Granted at boot from
-- NIMBUS_PLATFORM_ADMIN_EMAILS (idempotent promote, never demote — revoke
-- is a deliberate manual UPDATE), so the flag is data, not config lookup.
ALTER TABLE users ADD COLUMN is_platform_admin boolean NOT NULL DEFAULT false;

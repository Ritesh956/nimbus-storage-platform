-- Delegated org-admin tier (governance session #2): owner > admin > member.
-- Admins get oversight (usage view) and member management with hard limits
-- enforced in org.Service: they can only grant 'member' and only remove
-- plain members — elevated grants/removals stay owner-only.
ALTER TYPE member_role ADD VALUE IF NOT EXISTS 'admin';

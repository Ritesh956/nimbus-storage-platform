-- FR-4 (docs/01-srs.md §3.1): basic audit log of auth events (login,
-- refresh, logout). Kept separate from activity_events rather than reusing
-- it: activity_events is org-scoped (org_id NOT NULL) and target_id NOT
-- NULL, but auth events happen before/independent of any org context (a
-- user can belong to zero or many orgs, per the memberships table) and
-- have no file/folder target — forcing them into that schema would mean
-- nullable columns on a table every other module already relies on being
-- org-scoped. auth already owns users/refresh_tokens, so it owns this too.
CREATE TABLE auth_audit_log (
    id         bigserial PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_auth_audit_user_time ON auth_audit_log (user_id, created_at DESC);

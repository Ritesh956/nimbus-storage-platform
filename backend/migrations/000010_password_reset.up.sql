-- Backlog #14: password reset tokens. Single-use and short-lived (1h TTL
-- enforced in auth.Service); only the SHA-256 of the emailed token is
-- stored, same rationale as refresh_tokens.token_hash — a DB leak must not
-- hand out live reset links.
CREATE TABLE password_reset_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_password_reset_user ON password_reset_tokens (user_id);

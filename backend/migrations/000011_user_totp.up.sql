-- Backlog #15: TOTP 2FA. One row per enrolled user; a row with NULL
-- confirmed_at is a pending enrollment (setup shown, code not yet
-- verified) and does not gate login. The base32 secret is stored plaintext
-- because the server must derive codes from it on every verification —
-- same at-rest posture as the JWT secret (docs/01-srs.md §5 threat model).
CREATE TABLE user_totp (
    user_id      uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret       text NOT NULL,
    confirmed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

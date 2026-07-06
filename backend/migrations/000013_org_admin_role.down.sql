-- Postgres can't drop an enum value in place: demote admins, rebuild the
-- type without 'admin', swap it in.
UPDATE memberships SET role = 'member' WHERE role = 'admin';
CREATE TYPE member_role_old AS ENUM ('owner', 'member');
ALTER TABLE memberships ALTER COLUMN role DROP DEFAULT;
ALTER TABLE memberships ALTER COLUMN role TYPE member_role_old USING role::text::member_role_old;
DROP TYPE member_role;
ALTER TYPE member_role_old RENAME TO member_role;
ALTER TABLE memberships ALTER COLUMN role SET DEFAULT 'member';

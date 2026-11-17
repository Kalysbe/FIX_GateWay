-- Console users sign in with a plain login instead of an e-mail address.
ALTER TABLE users RENAME COLUMN email TO username;
ALTER TABLE users RENAME CONSTRAINT users_email_key TO users_username_key;

-- The session-revision trigger must follow the rename before any row changes.
CREATE OR REPLACE FUNCTION refresh_admin_session_revision() RETURNS trigger AS $$
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       OR NEW.role_code IS DISTINCT FROM OLD.role_code
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.username IS DISTINCT FROM OLD.username THEN
        NEW.updated_at := clock_timestamp();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- An existing e-mail login becomes the part before "@" (admin@localhost ->
-- admin), unless another user would end up with the same login; such rows keep
-- the full address. The trigger revokes the renamed users' sessions.
UPDATE users u SET username = split_part(u.username, '@', 1)
WHERE position('@' IN u.username) > 1
  AND NOT EXISTS (
      SELECT 1 FROM users o
      WHERE o.id <> u.id AND split_part(o.username, '@', 1) = split_part(u.username, '@', 1));

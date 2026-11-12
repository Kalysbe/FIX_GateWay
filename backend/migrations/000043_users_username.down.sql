-- Logins shortened by the up migration are not turned back into addresses.
CREATE OR REPLACE FUNCTION refresh_admin_session_revision() RETURNS trigger AS $$
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       OR NEW.role_code IS DISTINCT FROM OLD.role_code
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.email IS DISTINCT FROM OLD.email THEN
        NEW.updated_at := clock_timestamp();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE users RENAME CONSTRAINT users_username_key TO users_email_key;
ALTER TABLE users RENAME COLUMN username TO email;

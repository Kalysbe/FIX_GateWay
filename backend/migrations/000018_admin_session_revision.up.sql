-- Always revoke existing JWTs when security-sensitive user fields change,
-- including updates made outside the application.
CREATE FUNCTION refresh_admin_session_revision() RETURNS trigger AS $$
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

CREATE TRIGGER users_session_revision
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION refresh_admin_session_revision();

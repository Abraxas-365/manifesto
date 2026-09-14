-- Credential generations survive reinstatement. Legacy tokens are generation 0.
ALTER TABLE users ADD COLUMN credential_version BIGINT NOT NULL DEFAULT 0
    CHECK (credential_version >= 0);
ALTER TABLE refresh_tokens ADD COLUMN credential_version BIGINT NOT NULL DEFAULT 0
    CHECK (credential_version >= 0);

-- Already suspended memberships must not recover legacy tokens on reinstatement.
UPDATE users SET credential_version = 1 WHERE status = 'SUSPENDED';
UPDATE refresh_tokens SET is_revoked = true
WHERE user_id IN (SELECT id FROM users WHERE status = 'SUSPENDED');
UPDATE user_sessions SET expires_at = LEAST(expires_at, NOW())
WHERE user_id IN (SELECT id FROM users WHERE status = 'SUSPENDED');

-- Invalidation and status changes commit together, including direct SQL updates.
-- Preserve the generation on other updates so stale saves cannot reset it.
CREATE FUNCTION invalidate_suspended_user_credentials() RETURNS trigger AS $$
BEGIN
    NEW.credential_version := OLD.credential_version;
    IF NEW.status = 'SUSPENDED' AND OLD.status <> 'SUSPENDED' THEN
        NEW.credential_version := OLD.credential_version + 1;
        UPDATE refresh_tokens SET is_revoked = true
            WHERE user_id = OLD.id AND tenant_id = OLD.tenant_id;
        UPDATE user_sessions SET expires_at = LEAST(expires_at, NOW())
            WHERE user_id = OLD.id AND tenant_id = OLD.tenant_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER invalidate_user_credentials_on_suspension
BEFORE UPDATE ON users FOR EACH ROW
EXECUTE FUNCTION invalidate_suspended_user_credentials();

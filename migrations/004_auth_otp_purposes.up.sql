-- Keep signup verification codes separate from login credentials.
ALTER TABLE otps DROP CONSTRAINT chk_otp_purpose;
ALTER TABLE otps ADD CONSTRAINT chk_otp_purpose
    CHECK (purpose IN ('JOB_APPLICATION', 'VERIFICATION', 'SIGNUP', 'LOGIN'));

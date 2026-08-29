-- Link refresh tokens to sessions and remove dead session_token column

-- Add session_id to refresh_tokens to link tokens to sessions
ALTER TABLE refresh_tokens ADD COLUMN session_id UUID REFERENCES user_sessions(id) ON DELETE CASCADE;

-- Create index for efficient lookup by session_id
CREATE INDEX idx_refresh_tokens_session_id ON refresh_tokens(session_id) WHERE session_id IS NOT NULL;

-- Remove the dead session_token column from user_sessions
ALTER TABLE user_sessions DROP COLUMN IF EXISTS session_token;

-- Add optional role_id to invitations for role assignment on accept
ALTER TABLE invitations ADD COLUMN role_id VARCHAR(255) REFERENCES roles(id) ON DELETE SET NULL;

package authinfra

import (
	"context"
	"database/sql"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/jmoiron/sqlx"
)

// PostgresTokenRepository is the PostgreSQL implementation of TokenRepository
type PostgresTokenRepository struct {
	db *sqlx.DB
}

// NewPostgresTokenRepository creates a new token repository instance
func NewPostgresTokenRepository(db *sqlx.DB) auth.TokenRepository {
	return &PostgresTokenRepository{
		db: db,
	}
}

// SaveRefreshToken saves a new refresh token
func (r *PostgresTokenRepository) SaveRefreshToken(ctx context.Context, token auth.RefreshToken) error {
	query := `
		INSERT INTO refresh_tokens (
			id, token, user_id, tenant_id, expires_at, created_at, is_revoked
		) VALUES (
			:id, :token, :user_id, :tenant_id, :expires_at, :created_at, :is_revoked
		)`

	_, err := r.db.NamedExecContext(ctx, query, token)
	if err != nil {
		return errx.Wrap(err, "failed to save refresh token", errx.TypeInternal).
			WithDetail("user_id", token.UserID.String())
	}

	return nil
}

// FindRefreshToken finds a refresh token by its value
func (r *PostgresTokenRepository) FindRefreshToken(ctx context.Context, tokenValue string) (*auth.RefreshToken, error) {
	query := `
		SELECT 
			id, token, user_id, tenant_id, expires_at, created_at, is_revoked
		FROM refresh_tokens 
		WHERE token = $1 AND is_revoked = false`

	var token auth.RefreshToken
	err := r.db.GetContext(ctx, &token, query, tokenValue)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, auth.ErrInvalidRefreshToken()
		}
		return nil, errx.Wrap(err, "failed to find refresh token", errx.TypeInternal)
	}

	return &token, nil
}

// RevokeRefreshToken revokes a refresh token
func (r *PostgresTokenRepository) RevokeRefreshToken(ctx context.Context, tokenValue string) error {
	query := `
		UPDATE refresh_tokens 
		SET is_revoked = true 
		WHERE token = $1`

	result, err := r.db.ExecContext(ctx, query, tokenValue)
	if err != nil {
		return errx.Wrap(err, "failed to revoke refresh token", errx.TypeInternal)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return auth.ErrInvalidRefreshToken()
	}

	return nil
}

// RevokeAllUserTokens revokes all tokens for a user
func (r *PostgresTokenRepository) RevokeAllUserTokens(ctx context.Context, userID kernel.UserID) error {
	query := `
		UPDATE refresh_tokens 
		SET is_revoked = true 
		WHERE user_id = $1 AND is_revoked = false`

	_, err := r.db.ExecContext(ctx, query, userID.String())
	if err != nil {
		return errx.Wrap(err, "failed to revoke all user tokens", errx.TypeInternal).
			WithDetail("user_id", userID.String())
	}

	return nil
}

// CleanExpiredTokens deletes expired tokens (for maintenance)
func (r *PostgresTokenRepository) CleanExpiredTokens(ctx context.Context) error {
	query := `
		DELETE FROM refresh_tokens 
		WHERE expires_at < NOW() OR is_revoked = true`

	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return errx.Wrap(err, "failed to clean expired tokens", errx.TypeInternal)
	}

	return nil
}

// CountActiveTokens counts active tokens for a user
func (r *PostgresTokenRepository) CountActiveTokens(ctx context.Context, userID kernel.UserID) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM refresh_tokens 
		WHERE user_id = $1 AND is_revoked = false AND expires_at > NOW()`

	var count int
	err := r.db.GetContext(ctx, &count, query, userID.String())
	if err != nil {
		return 0, errx.Wrap(err, "failed to count active tokens", errx.TypeInternal).
			WithDetail("user_id", userID.String())
	}

	return count, nil
}

// GetActiveTokensByUser retrieves all active tokens for a user
func (r *PostgresTokenRepository) GetActiveTokensByUser(ctx context.Context, userID kernel.UserID) ([]*auth.RefreshToken, error) {
	query := `
		SELECT 
			id, token, user_id, tenant_id, expires_at, created_at, is_revoked
		FROM refresh_tokens 
		WHERE user_id = $1 AND is_revoked = false AND expires_at > NOW()
		ORDER BY created_at DESC`

	var tokens []auth.RefreshToken
	err := r.db.SelectContext(ctx, &tokens, query, userID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to get active tokens", errx.TypeInternal).
			WithDetail("user_id", userID.String())
	}

	// Convert to pointer slice
	result := make([]*auth.RefreshToken, len(tokens))
	for i := range tokens {
		result[i] = &tokens[i]
	}

	return result, nil
}

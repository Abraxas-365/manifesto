package authinfra

import (
	"context"
	"database/sql"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/jmoiron/sqlx"
)

// PostgresPasswordResetRepository implementación de PostgreSQL para PasswordResetRepository
type PostgresPasswordResetRepository struct {
	db *sqlx.DB
}

// NewPostgresPasswordResetRepository crea una nueva instancia del repositorio de reset de contraseña
func NewPostgresPasswordResetRepository(db *sqlx.DB) auth.PasswordResetRepository {
	return &PostgresPasswordResetRepository{
		db: db,
	}
}

// SaveResetToken guarda un token de reset de contraseña
func (r *PostgresPasswordResetRepository) SaveResetToken(ctx context.Context, token auth.PasswordResetToken) error {
	query := `
		INSERT INTO password_reset_tokens (
			id, token, user_id, expires_at, created_at, is_used
		) VALUES (
			:id, :token, :user_id, :expires_at, :created_at, :is_used
		)`

	_, err := r.db.NamedExecContext(ctx, query, token)
	if err != nil {
		return errx.Wrap(err, "failed to save reset token", errx.TypeInternal).
			WithDetail("user_id", token.UserID.String())
	}

	return nil
}

// FindResetToken busca un token de reset por su valor
func (r *PostgresPasswordResetRepository) FindResetToken(ctx context.Context, tokenValue string) (*auth.PasswordResetToken, error) {
	query := `
		SELECT 
			id, token, user_id, expires_at, created_at, is_used
		FROM password_reset_tokens 
		WHERE token = $1 AND is_used = false AND expires_at > NOW()`

	var token auth.PasswordResetToken
	err := r.db.GetContext(ctx, &token, query, tokenValue)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errx.New("reset token not found or invalid", errx.TypeNotFound)
		}
		return nil, errx.Wrap(err, "failed to find reset token", errx.TypeInternal)
	}

	return &token, nil
}

// ConsumeResetToken marca un token como usado
func (r *PostgresPasswordResetRepository) ConsumeResetToken(ctx context.Context, tokenValue string) error {
	query := `
		UPDATE password_reset_tokens 
		SET is_used = true 
		WHERE token = $1 AND is_used = false AND expires_at > NOW()`

	result, err := r.db.ExecContext(ctx, query, tokenValue)
	if err != nil {
		return errx.Wrap(err, "failed to consume reset token", errx.TypeInternal)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return errx.New("reset token not found or already used", errx.TypeNotFound)
	}

	return nil
}

// CleanExpiredResetTokens limpia tokens expirados o usados (para mantenimiento)
func (r *PostgresPasswordResetRepository) CleanExpiredResetTokens(ctx context.Context) error {
	query := `
		DELETE FROM password_reset_tokens 
		WHERE expires_at < NOW() OR is_used = true`

	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return errx.Wrap(err, "failed to clean expired reset tokens", errx.TypeInternal)
	}

	return nil
}

// RevokeAllUserResetTokens revoca todos los tokens de reset de un usuario
func (r *PostgresPasswordResetRepository) RevokeAllUserResetTokens(ctx context.Context, userID string) error {
	query := `
		UPDATE password_reset_tokens 
		SET is_used = true 
		WHERE user_id = $1 AND is_used = false`

	_, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return errx.Wrap(err, "failed to revoke all user reset tokens", errx.TypeInternal).
			WithDetail("user_id", userID)
	}

	return nil
}

// CountActiveResetTokens cuenta los tokens activos de reset de un usuario
func (r *PostgresPasswordResetRepository) CountActiveResetTokens(ctx context.Context, userID string) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM password_reset_tokens 
		WHERE user_id = $1 AND is_used = false AND expires_at > NOW()`

	var count int
	err := r.db.GetContext(ctx, &count, query, userID)
	if err != nil {
		return 0, errx.Wrap(err, "failed to count active reset tokens", errx.TypeInternal).
			WithDetail("user_id", userID)
	}

	return count, nil
}

// HasRecentResetToken verifica si un usuario tiene un token reciente (anti-spam)
func (r *PostgresPasswordResetRepository) HasRecentResetToken(ctx context.Context, userID string, withinMinutes int) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM password_reset_tokens 
			WHERE user_id = $1 
			AND created_at > NOW() - INTERVAL '%d minutes'
			AND is_used = false
		)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, userID, withinMinutes)
	if err != nil {
		return false, errx.Wrap(err, "failed to check recent reset token", errx.TypeInternal).
			WithDetail("user_id", userID)
	}

	return exists, nil
}

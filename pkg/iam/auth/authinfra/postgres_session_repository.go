package authinfra

import (
	"context"
	"database/sql"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/jmoiron/sqlx"
)

// PostgresSessionRepository implementación de PostgreSQL para SessionRepository
type PostgresSessionRepository struct {
	db *sqlx.DB
}

// NewPostgresSessionRepository crea una nueva instancia del repositorio de sesiones
func NewPostgresSessionRepository(db *sqlx.DB) auth.SessionRepository {
	return &PostgresSessionRepository{
		db: db,
	}
}

// SaveSession guarda una nueva sesión de usuario
func (r *PostgresSessionRepository) SaveSession(ctx context.Context, session auth.UserSession) error {
	query := `
		INSERT INTO user_sessions (
			id, user_id, tenant_id, session_token, ip_address, 
			user_agent, expires_at, created_at, last_activity
		) VALUES (
			:id, :user_id, :tenant_id, :session_token, :ip_address,
			:user_agent, :expires_at, :created_at, :last_activity
		)`

	_, err := r.db.NamedExecContext(ctx, query, session)
	if err != nil {
		return errx.Wrap(err, "failed to save session", errx.TypeInternal).
			WithDetail("user_id", session.UserID.String())
	}

	return nil
}

// FindSession busca una sesión por ID
func (r *PostgresSessionRepository) FindSession(ctx context.Context, sessionID string) (*auth.UserSession, error) {
	query := `
		SELECT 
			id, user_id, tenant_id, session_token, ip_address,
			user_agent, expires_at, created_at, last_activity
		FROM user_sessions 
		WHERE id = $1`

	var session auth.UserSession
	err := r.db.GetContext(ctx, &session, query, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errx.New("session not found", errx.TypeNotFound).
				WithDetail("session_id", sessionID)
		}
		return nil, errx.Wrap(err, "failed to find session", errx.TypeInternal).
			WithDetail("session_id", sessionID)
	}

	return &session, nil
}

// FindSessionByToken busca una sesión por token
func (r *PostgresSessionRepository) FindSessionByToken(ctx context.Context, sessionToken string) (*auth.UserSession, error) {
	query := `
		SELECT 
			id, user_id, tenant_id, session_token, ip_address,
			user_agent, expires_at, created_at, last_activity
		FROM user_sessions 
		WHERE session_token = $1 AND expires_at > NOW()`

	var session auth.UserSession
	err := r.db.GetContext(ctx, &session, query, sessionToken)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errx.New("session not found", errx.TypeNotFound)
		}
		return nil, errx.Wrap(err, "failed to find session by token", errx.TypeInternal)
	}

	return &session, nil
}

// FindUserSessions busca todas las sesiones activas de un usuario
func (r *PostgresSessionRepository) FindUserSessions(ctx context.Context, userID kernel.UserID) ([]*auth.UserSession, error) {
	query := `
		SELECT 
			id, user_id, tenant_id, session_token, ip_address,
			user_agent, expires_at, created_at, last_activity
		FROM user_sessions 
		WHERE user_id = $1 AND expires_at > NOW()
		ORDER BY last_activity DESC`

	var sessions []auth.UserSession
	err := r.db.SelectContext(ctx, &sessions, query, userID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find user sessions", errx.TypeInternal).
			WithDetail("user_id", userID.String())
	}

	// Convertir a slice de punteros
	result := make([]*auth.UserSession, len(sessions))
	for i := range sessions {
		result[i] = &sessions[i]
	}

	return result, nil
}

// UpdateSessionActivity actualiza la última actividad de una sesión
func (r *PostgresSessionRepository) UpdateSessionActivity(ctx context.Context, sessionID string) error {
	query := `
		UPDATE user_sessions 
		SET last_activity = NOW() 
		WHERE id = $1 AND expires_at > NOW()`

	result, err := r.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return errx.Wrap(err, "failed to update session activity", errx.TypeInternal).
			WithDetail("session_id", sessionID)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return errx.New("session not found or expired", errx.TypeNotFound).
			WithDetail("session_id", sessionID)
	}

	return nil
}

// RevokeSession revoca una sesión específica
func (r *PostgresSessionRepository) RevokeSession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM user_sessions WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return errx.Wrap(err, "failed to revoke session", errx.TypeInternal).
			WithDetail("session_id", sessionID)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return errx.New("session not found", errx.TypeNotFound).
			WithDetail("session_id", sessionID)
	}

	return nil
}

// RevokeAllUserSessions revoca todas las sesiones de un usuario
func (r *PostgresSessionRepository) RevokeAllUserSessions(ctx context.Context, userID kernel.UserID) error {
	query := `DELETE FROM user_sessions WHERE user_id = $1`

	_, err := r.db.ExecContext(ctx, query, userID.String())
	if err != nil {
		return errx.Wrap(err, "failed to revoke all user sessions", errx.TypeInternal).
			WithDetail("user_id", userID.String())
	}

	return nil
}

// CleanExpiredSessions elimina sesiones expiradas (para mantenimiento)
func (r *PostgresSessionRepository) CleanExpiredSessions(ctx context.Context) error {
	query := `DELETE FROM user_sessions WHERE expires_at < NOW()`

	_, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return errx.Wrap(err, "failed to clean expired sessions", errx.TypeInternal)
	}

	return nil
}

// ExtendSession extiende la expiración de una sesión
func (r *PostgresSessionRepository) ExtendSession(ctx context.Context, sessionID string, duration time.Duration) error {
	query := `
		UPDATE user_sessions 
		SET expires_at = expires_at + $2::interval,
		    last_activity = NOW()
		WHERE id = $1 AND expires_at > NOW()`

	result, err := r.db.ExecContext(ctx, query, sessionID, duration)
	if err != nil {
		return errx.Wrap(err, "failed to extend session", errx.TypeInternal).
			WithDetail("session_id", sessionID).
			WithDetail("duration", duration.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return errx.New("session not found or expired", errx.TypeNotFound).
			WithDetail("session_id", sessionID)
	}

	return nil
}

// CountActiveSessions cuenta las sesiones activas de un usuario
func (r *PostgresSessionRepository) CountActiveSessions(ctx context.Context, userID kernel.UserID) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM user_sessions 
		WHERE user_id = $1 AND expires_at > NOW()`

	var count int
	err := r.db.GetContext(ctx, &count, query, userID.String())
	if err != nil {
		return 0, errx.Wrap(err, "failed to count active sessions", errx.TypeInternal).
			WithDetail("user_id", userID.String())
	}

	return count, nil
}

// GetSessionsByIPAddress obtiene sesiones por dirección IP (para seguridad)
func (r *PostgresSessionRepository) GetSessionsByIPAddress(ctx context.Context, ipAddress string) ([]*auth.UserSession, error) {
	query := `
		SELECT 
			id, user_id, tenant_id, session_token, ip_address,
			user_agent, expires_at, created_at, last_activity
		FROM user_sessions 
		WHERE ip_address = $1 AND expires_at > NOW()
		ORDER BY created_at DESC`

	var sessions []auth.UserSession
	err := r.db.SelectContext(ctx, &sessions, query, ipAddress)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get sessions by IP", errx.TypeInternal).
			WithDetail("ip_address", ipAddress)
	}

	// Convertir a slice de punteros
	result := make([]*auth.UserSession, len(sessions))
	for i := range sessions {
		result[i] = &sessions[i]
	}

	return result, nil
}

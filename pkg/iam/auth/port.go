package auth

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

// TokenRepository define el contrato para la persistencia de tokens
type TokenRepository interface {
	SaveRefreshToken(ctx context.Context, token RefreshToken) error
	FindRefreshToken(ctx context.Context, tokenValue string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenValue string) error
	RevokeAllUserTokens(ctx context.Context, userID kernel.UserID) error
	CleanExpiredTokens(ctx context.Context) error
}

// SessionRepository define el contrato para la persistencia de sesiones
type SessionRepository interface {
	SaveSession(ctx context.Context, session UserSession) error
	FindSession(ctx context.Context, sessionID string) (*UserSession, error)
	FindUserSessions(ctx context.Context, userID kernel.UserID) ([]*UserSession, error)
	UpdateSessionActivity(ctx context.Context, sessionID string) error
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeAllUserSessions(ctx context.Context, userID kernel.UserID) error
	CleanExpiredSessions(ctx context.Context) error
}

// PasswordResetRepository define el contrato para tokens de reset de contraseña
type PasswordResetRepository interface {
	SaveResetToken(ctx context.Context, token PasswordResetToken) error
	FindResetToken(ctx context.Context, tokenValue string) (*PasswordResetToken, error)
	ConsumeResetToken(ctx context.Context, tokenValue string) error
	CleanExpiredResetTokens(ctx context.Context) error
}

// TokenService define el contrato para el manejo de tokens JWT
type TokenService interface {
	GenerateAccessToken(userID kernel.UserID, tenantID kernel.TenantID, claims map[string]any) (string, error)
	ValidateAccessToken(token string) (*TokenClaims, error)
	GenerateRefreshToken(userID kernel.UserID) (string, error)
}

// AuditService define el contrato para logs de autenticación
type AuditService interface {
	LogLoginAttempt(ctx context.Context, userID kernel.UserID, success bool, ipAddress string) error
	LogPasswordChange(ctx context.Context, userID kernel.UserID, ipAddress string) error
	LogTokenRefresh(ctx context.Context, userID kernel.UserID, ipAddress string) error
}

// Invitation representa una invitación (para evitar dependencia circular)
type Invitation interface {
	GetID() string
	GetTenantID() kernel.TenantID
	GetEmail() string
	CanBeAccepted() bool
	IsExpired() bool
	Accept(userID kernel.UserID) error
}

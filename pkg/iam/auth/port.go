package auth

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

// TokenRepository defines the contract for token persistence
type TokenRepository interface {
	SaveRefreshToken(ctx context.Context, token RefreshToken) error
	FindRefreshToken(ctx context.Context, tokenValue string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenValue string) error
	RevokeAllUserTokens(ctx context.Context, userID kernel.UserID) error
	CleanExpiredTokens(ctx context.Context) error
}

// SessionRepository defines the contract for session persistence
type SessionRepository interface {
	SaveSession(ctx context.Context, session UserSession) error
	FindSession(ctx context.Context, sessionID string) (*UserSession, error)
	FindUserSessions(ctx context.Context, userID kernel.UserID) ([]*UserSession, error)
	UpdateSessionActivity(ctx context.Context, sessionID string) error
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeAllUserSessions(ctx context.Context, userID kernel.UserID) error
	CleanExpiredSessions(ctx context.Context) error
}

// PasswordResetRepository defines the contract for password reset tokens
type PasswordResetRepository interface {
	SaveResetToken(ctx context.Context, token PasswordResetToken) error
	FindResetToken(ctx context.Context, tokenValue string) (*PasswordResetToken, error)
	ConsumeResetToken(ctx context.Context, tokenValue string) error
	CleanExpiredResetTokens(ctx context.Context) error
}

// TokenService defines the contract for JWT token management
type TokenService interface {
	GenerateAccessToken(userID kernel.UserID, tenantID kernel.TenantID, claims map[string]any) (string, error)
	ValidateAccessToken(token string) (*TokenClaims, error)
	GenerateRefreshToken(userID kernel.UserID) (string, error)
}

// AuditService defines the contract for authentication audit logging
type AuditService interface {
	LogLoginAttempt(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, method string, success bool, ip string, userAgent string)
	LogLogout(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, ip string)
	LogTokenRefresh(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, ip string)
	LogOTPVerification(ctx context.Context, contact string, success bool, ip string)
	LogAccountCreated(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, method string, ip string)
	LogAccountLinked(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, method string, ip string)
}

// Invitation represents an invitation (to avoid circular dependency)
type Invitation interface {
	GetID() string
	GetTenantID() kernel.TenantID
	GetEmail() string
	CanBeAccepted() bool
	IsExpired() bool
	Accept(userID kernel.UserID) error
}

package auth

import (
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

// ============================================================================
// Token Types
// ============================================================================

// RefreshToken represents a refresh token
type RefreshToken struct {
	ID        string          `db:"id" json:"id"`
	Token     string          `db:"token" json:"token"`
	UserID    kernel.UserID   `db:"user_id" json:"user_id"`
	TenantID  kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	ExpiresAt time.Time       `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`
	IsRevoked bool            `db:"is_revoked" json:"is_revoked"`
}

// UserSession represents a user session
type UserSession struct {
	ID           string          `db:"id" json:"id"`
	UserID       kernel.UserID   `db:"user_id" json:"user_id"`
	TenantID     kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	SessionToken string          `db:"session_token" json:"session_token"`
	IPAddress    string          `db:"ip_address" json:"ip_address"`
	UserAgent    string          `db:"user_agent" json:"user_agent"`
	ExpiresAt    time.Time       `db:"expires_at" json:"expires_at"`
	CreatedAt    time.Time       `db:"created_at" json:"created_at"`
	LastActivity time.Time       `db:"last_activity" json:"last_activity"`
}

// PasswordResetToken represents a password reset token
type PasswordResetToken struct {
	ID        string        `db:"id" json:"id"`
	Token     string        `db:"token" json:"token"`
	UserID    kernel.UserID `db:"user_id" json:"user_id"`
	ExpiresAt time.Time     `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time     `db:"created_at" json:"created_at"`
	IsUsed    bool          `db:"is_used" json:"is_used"`
}

// TokenClaims represents JWT claims
type TokenClaims struct {
	UserID    kernel.UserID   `json:"user_id"`
	TenantID  kernel.TenantID `json:"tenant_id"`
	Email     string          `json:"email"`
	Name      string          `json:"name"`
	Scopes    []string        `json:"scopes"`
	IssuedAt  time.Time       `json:"iat"`
	ExpiresAt time.Time       `json:"exp"`
}

// ============================================================================
// Domain Methods
// ============================================================================

// IsExpired checks if the refresh token has expired
func (r *RefreshToken) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// IsValid checks if the refresh token is valid
func (r *RefreshToken) IsValid() bool {
	return !r.IsRevoked && !r.IsExpired()
}

// IsExpired checks if the session has expired
func (s *UserSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// UpdateActivity updates the session's last activity
func (s *UserSession) UpdateActivity() {
	s.LastActivity = time.Now()
}

// IsExpired checks if the reset token has expired
func (p *PasswordResetToken) IsExpired() bool {
	return time.Now().After(p.ExpiresAt)
}

// IsValid checks if the reset token is valid
func (p *PasswordResetToken) IsValid() bool {
	return !p.IsUsed && !p.IsExpired()
}

// MarkAsUsed marks the token as used
func (p *PasswordResetToken) MarkAsUsed() {
	p.IsUsed = true
}

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("AUTH")

var (
	CodeInvalidRefreshToken      = ErrRegistry.Register("INVALID_REFRESH_TOKEN", errx.TypeAuthorization, http.StatusUnauthorized, "Invalid refresh token")
	CodeExpiredRefreshToken      = ErrRegistry.Register("EXPIRED_REFRESH_TOKEN", errx.TypeAuthorization, http.StatusUnauthorized, "Expired refresh token")
	CodeInvalidOAuthProvider     = ErrRegistry.Register("INVALID_OAUTH_PROVIDER", errx.TypeValidation, http.StatusBadRequest, "Invalid OAuth provider")
	CodeOAuthAuthorizationFailed = ErrRegistry.Register("OAUTH_AUTHORIZATION_FAILED", errx.TypeExternal, http.StatusBadRequest, "OAuth authorization failed")
	CodeInvalidState             = ErrRegistry.Register("INVALID_STATE", errx.TypeValidation, http.StatusBadRequest, "Invalid OAuth state")
	CodeTokenGenerationFailed    = ErrRegistry.Register("TOKEN_GENERATION_FAILED", errx.TypeInternal, http.StatusInternalServerError, "Token generation failed")
	CodeTokenValidationFailed    = ErrRegistry.Register("TOKEN_VALIDATION_FAILED", errx.TypeAuthorization, http.StatusUnauthorized, "Token validation failed")
	CodeOAuthCallbackError       = ErrRegistry.Register("OAUTH_CALLBACK_ERROR", errx.TypeExternal, http.StatusBadRequest, "OAuth callback error")
)

// Helper functions
func ErrInvalidRefreshToken() *errx.Error {
	return ErrRegistry.New(CodeInvalidRefreshToken)
}

func ErrExpiredRefreshToken() *errx.Error {
	return ErrRegistry.New(CodeExpiredRefreshToken)
}

func ErrInvalidOAuthProvider() *errx.Error {
	return ErrRegistry.New(CodeInvalidOAuthProvider)
}

func ErrOAuthAuthorizationFailed() *errx.Error {
	return ErrRegistry.New(CodeOAuthAuthorizationFailed)
}

func ErrInvalidState() *errx.Error {
	return ErrRegistry.New(CodeInvalidState)
}

func ErrTokenGenerationFailed() *errx.Error {
	return ErrRegistry.New(CodeTokenGenerationFailed)
}

func ErrTokenValidationFailed() *errx.Error {
	return ErrRegistry.New(CodeTokenValidationFailed)
}

func ErrOAuthCallbackError() *errx.Error {
	return ErrRegistry.New(CodeOAuthCallbackError)
}

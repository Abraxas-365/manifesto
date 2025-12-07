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

// RefreshToken representa un token de refresh
type RefreshToken struct {
	ID        string          `db:"id" json:"id"`
	Token     string          `db:"token" json:"token"`
	UserID    kernel.UserID   `db:"user_id" json:"user_id"`
	TenantID  kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	ExpiresAt time.Time       `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`
	IsRevoked bool            `db:"is_revoked" json:"is_revoked"`
}

// UserSession representa una sesión de usuario
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

// PasswordResetToken representa un token para resetear contraseña
type PasswordResetToken struct {
	ID        string        `db:"id" json:"id"`
	Token     string        `db:"token" json:"token"`
	UserID    kernel.UserID `db:"user_id" json:"user_id"`
	ExpiresAt time.Time     `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time     `db:"created_at" json:"created_at"`
	IsUsed    bool          `db:"is_used" json:"is_used"`
}

// TokenClaims representa los claims de un JWT
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

// IsExpired verifica si el refresh token ha expirado
func (r *RefreshToken) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// IsValid verifica si el refresh token es válido
func (r *RefreshToken) IsValid() bool {
	return !r.IsRevoked && !r.IsExpired()
}

// IsExpired verifica si la sesión ha expirado
func (s *UserSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// UpdateActivity actualiza la última actividad de la sesión
func (s *UserSession) UpdateActivity() {
	s.LastActivity = time.Now()
}

// IsExpired verifica si el token de reset ha expirado
func (p *PasswordResetToken) IsExpired() bool {
	return time.Now().After(p.ExpiresAt)
}

// IsValid verifica si el token de reset es válido
func (p *PasswordResetToken) IsValid() bool {
	return !p.IsUsed && !p.IsExpired()
}

// MarkAsUsed marca el token como usado
func (p *PasswordResetToken) MarkAsUsed() {
	p.IsUsed = true
}

// ============================================================================
// Error Registry - Errores específicos de Auth
// ============================================================================

var ErrRegistry = errx.NewRegistry("AUTH")

// Códigos de error
var (
	CodeInvalidRefreshToken      = ErrRegistry.Register("INVALID_REFRESH_TOKEN", errx.TypeAuthorization, http.StatusUnauthorized, "Refresh token inválido")
	CodeExpiredRefreshToken      = ErrRegistry.Register("EXPIRED_REFRESH_TOKEN", errx.TypeAuthorization, http.StatusUnauthorized, "Refresh token expirado")
	CodeInvalidOAuthProvider     = ErrRegistry.Register("INVALID_OAUTH_PROVIDER", errx.TypeValidation, http.StatusBadRequest, "Proveedor OAuth no válido")
	CodeOAuthAuthorizationFailed = ErrRegistry.Register("OAUTH_AUTHORIZATION_FAILED", errx.TypeExternal, http.StatusBadRequest, "Falló la autorización OAuth")
	CodeInvalidState             = ErrRegistry.Register("INVALID_STATE", errx.TypeValidation, http.StatusBadRequest, "Estado OAuth inválido")
	CodeTokenGenerationFailed    = ErrRegistry.Register("TOKEN_GENERATION_FAILED", errx.TypeInternal, http.StatusInternalServerError, "Error al generar token")
	CodeTokenValidationFailed    = ErrRegistry.Register("TOKEN_VALIDATION_FAILED", errx.TypeAuthorization, http.StatusUnauthorized, "Error al validar token")
	CodeOAuthCallbackError       = ErrRegistry.Register("OAUTH_CALLBACK_ERROR", errx.TypeExternal, http.StatusBadRequest, "Error en el callback OAuth")
)

// Helper functions para crear errores
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

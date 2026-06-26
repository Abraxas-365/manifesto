// pkg/auth/config.go
package auth

import (
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
)

// Config holds the full authentication module configuration
type Config struct {
	JWT   JWTConfig    `json:"jwt" yaml:"jwt"`
	OAuth OAuthConfigs `json:"oauth" yaml:"oauth"`
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	SecretKey       string        `json:"secret_key" yaml:"secret_key"`
	AccessTokenTTL  time.Duration `json:"access_token_ttl" yaml:"access_token_ttl"`
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl" yaml:"refresh_token_ttl"`
	Issuer          string        `json:"issuer" yaml:"issuer"`
}

// OAuthConfig holds base OAuth configuration
type OAuthConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// OAuthConfigs holds configuration for all OAuth providers
type OAuthConfigs struct {
	Google    OAuthConfig `json:"google" yaml:"google"`
	Microsoft OAuthConfig `json:"microsoft" yaml:"microsoft"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		JWT: JWTConfig{
			AccessTokenTTL:  15 * time.Minute,
			RefreshTokenTTL: 7 * 24 * time.Hour,
			Issuer:          "facturamelo",
		},
		OAuth: OAuthConfigs{
			Google: OAuthConfig{
				Scopes: []string{"openid", "email", "profile"},
			},
			Microsoft: OAuthConfig{
				Scopes: []string{"openid", "email", "profile", "User.Read"},
			},
		},
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.JWT.SecretKey == "" {
		return ErrMissingJWTSecret()
	}

	if len(c.JWT.SecretKey) < 32 {
		return ErrWeakJWTSecret()
	}

	if c.JWT.AccessTokenTTL <= 0 {
		return ErrInvalidTokenTTL().WithDetail("token_type", "access")
	}

	if c.JWT.RefreshTokenTTL <= 0 {
		return ErrInvalidTokenTTL().WithDetail("token_type", "refresh")
	}

	// Validate OAuth config if present
	if err := c.OAuth.Google.Validate("Google"); err != nil {
		return err
	}

	if err := c.OAuth.Microsoft.Validate("Microsoft"); err != nil {
		return err
	}

	return nil
}

// Validate validates the OAuth configuration
func (oc *OAuthConfig) Validate(provider string) error {
	// Only validate if configured (allows optional providers)
	if oc.ClientID == "" && oc.ClientSecret == "" {
		return nil // Provider not configured, that's fine
	}

	if oc.ClientID == "" {
		return ErrMissingOAuthClientID().WithDetail("provider", provider)
	}

	if oc.ClientSecret == "" {
		return ErrMissingOAuthClientSecret().WithDetail("provider", provider)
	}

	if oc.RedirectURL == "" {
		return ErrMissingOAuthRedirectURL().WithDetail("provider", provider)
	}

	if len(oc.Scopes) == 0 {
		return ErrMissingOAuthScopes().WithDetail("provider", provider)
	}

	return nil
}

// IsEnabled checks whether the OAuth provider is enabled
func (oc *OAuthConfig) IsEnabled() bool {
	return oc.ClientID != "" && oc.ClientSecret != ""
}

// GetEnabledProviders returns a list of enabled OAuth providers
func (oc *OAuthConfigs) GetEnabledProviders() []string {
	var enabled []string

	if oc.Google.IsEnabled() {
		enabled = append(enabled, "google")
	}

	if oc.Microsoft.IsEnabled() {
		enabled = append(enabled, "microsoft")
	}

	return enabled
}

// Config error codes
var (
	CodeMissingJWTSecret         = ErrRegistry.Register("MISSING_JWT_SECRET", errx.TypeValidation, http.StatusBadRequest, "JWT secret key is required")
	CodeWeakJWTSecret            = ErrRegistry.Register("WEAK_JWT_SECRET", errx.TypeValidation, http.StatusBadRequest, "JWT secret key must be at least 32 characters")
	CodeInvalidTokenTTL          = ErrRegistry.Register("INVALID_TOKEN_TTL", errx.TypeValidation, http.StatusBadRequest, "Invalid token TTL")
	CodeMissingOAuthClientID     = ErrRegistry.Register("MISSING_OAUTH_CLIENT_ID", errx.TypeValidation, http.StatusBadRequest, "OAuth client ID is required")
	CodeMissingOAuthClientSecret = ErrRegistry.Register("MISSING_OAUTH_CLIENT_SECRET", errx.TypeValidation, http.StatusBadRequest, "OAuth client secret is required")
	CodeMissingOAuthRedirectURL  = ErrRegistry.Register("MISSING_OAUTH_REDIRECT_URL", errx.TypeValidation, http.StatusBadRequest, "OAuth redirect URL is required")
	CodeMissingOAuthScopes       = ErrRegistry.Register("MISSING_OAUTH_SCOPES", errx.TypeValidation, http.StatusBadRequest, "OAuth scopes are required")
)

// Helper functions for creating config errors
func ErrMissingJWTSecret() *errx.Error {
	return ErrRegistry.New(CodeMissingJWTSecret)
}

func ErrWeakJWTSecret() *errx.Error {
	return ErrRegistry.New(CodeWeakJWTSecret)
}

func ErrInvalidTokenTTL() *errx.Error {
	return ErrRegistry.New(CodeInvalidTokenTTL)
}

func ErrMissingOAuthClientID() *errx.Error {
	return ErrRegistry.New(CodeMissingOAuthClientID)
}

func ErrMissingOAuthClientSecret() *errx.Error {
	return ErrRegistry.New(CodeMissingOAuthClientSecret)
}

func ErrMissingOAuthRedirectURL() *errx.Error {
	return ErrRegistry.New(CodeMissingOAuthRedirectURL)
}

func ErrMissingOAuthScopes() *errx.Error {
	return ErrRegistry.New(CodeMissingOAuthScopes)
}

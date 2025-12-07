// pkg/auth/oauth.go
package auth

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/iam"
)

// ============================================================================
// OAuth Types
// ============================================================================

// OAuthUserInfo información del usuario desde el proveedor OAuth
type OAuthUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	EmailVerified bool   `json:"email_verified"`
}

// OAuthService define el contrato para servicios OAuth
type OAuthService interface {
	GetAuthURL(state string) string
	ExchangeToken(ctx context.Context, code string) (*OAuthTokenResponse, error)
	GetUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfo, error)
	ValidateState(state string) bool
	GetProvider() iam.OAuthProvider
}

// OAuthTokenResponse respuesta del intercambio de código por token
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
}

// StateManager maneja la validación de estados OAuth
type StateManager interface {
	GenerateState() string
	ValidateState(state string) bool
	StoreState(ctx context.Context, state string, data map[string]any) error
	GetStateData(ctx context.Context, state string) (map[string]any, error)
}

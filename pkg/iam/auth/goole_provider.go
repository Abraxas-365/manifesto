package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam"
)

const (
	GoogleAuthURL     = "https://accounts.google.com/o/oauth2/auth"
	GoogleTokenURL    = "https://oauth2.googleapis.com/token"
	GoogleUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
)

// GoogleOAuthService implementación del servicio OAuth para Google
type GoogleOAuthService struct {
	config       OAuthConfig
	httpClient   *http.Client
	stateManager StateManager
}

// NewGoogleOAuthService crea una nueva instancia del servicio Google OAuth
func NewGoogleOAuthService(config OAuthConfig, stateManager StateManager) *GoogleOAuthService {
	if len(config.Scopes) == 0 {
		config.Scopes = []string{"openid", "email", "profile"}
	}

	return &GoogleOAuthService{
		config:       config,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		stateManager: stateManager,
	}
}

// GetProvider retorna el proveedor OAuth
func (g *GoogleOAuthService) GetProvider() iam.OAuthProvider {
	return iam.OAuthProviderGoogle
}

// GetAuthURL genera la URL de autorización de Google
func (g *GoogleOAuthService) GetAuthURL(state string) string {
	params := url.Values{
		"client_id":     {g.config.ClientID},
		"redirect_uri":  {g.config.RedirectURL},
		"scope":         {strings.Join(g.config.Scopes, " ")},
		"response_type": {"code"},
		"state":         {state},
		"access_type":   {"offline"}, // Para obtener refresh token
		"prompt":        {"consent"}, // Forzar consent para obtener refresh token
	}

	return fmt.Sprintf("%s?%s", GoogleAuthURL, params.Encode())
}

// ValidateState valida el estado OAuth
func (g *GoogleOAuthService) ValidateState(state string) bool {
	return g.stateManager.ValidateState(state)
}

// ExchangeToken intercambia el código de autorización por tokens
func (g *GoogleOAuthService) ExchangeToken(ctx context.Context, code string) (*OAuthTokenResponse, error) {
	data := url.Values{
		"client_id":     {g.config.ClientID},
		"client_secret": {g.config.ClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {g.config.RedirectURL},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", GoogleTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, errx.Wrap(err, "failed to create token request", errx.TypeInternal)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, errx.Wrap(err, "failed to exchange token", errx.TypeExternal)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrOAuthAuthorizationFailed().
			WithDetail("status_code", resp.StatusCode).
			WithDetail("provider", "google")
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, errx.Wrap(err, "failed to decode token response", errx.TypeExternal)
	}

	return &tokenResp, nil
}

// GetUserInfo obtiene la información del usuario desde Google
func (g *GoogleOAuthService) GetUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", GoogleUserInfoURL, nil)
	if err != nil {
		return nil, errx.Wrap(err, "failed to create user info request", errx.TypeInternal)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get user info", errx.TypeExternal)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrOAuthAuthorizationFailed().
			WithDetail("status_code", resp.StatusCode).
			WithDetail("provider", "google").
			WithDetail("endpoint", "userinfo")
	}

	var googleUser struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		EmailVerified bool   `json:"verified_email"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&googleUser); err != nil {
		return nil, errx.Wrap(err, "failed to decode user info", errx.TypeExternal)
	}

	return &OAuthUserInfo{
		ID:            googleUser.ID,
		Email:         googleUser.Email,
		Name:          googleUser.Name,
		Picture:       googleUser.Picture,
		EmailVerified: googleUser.EmailVerified,
	}, nil
}

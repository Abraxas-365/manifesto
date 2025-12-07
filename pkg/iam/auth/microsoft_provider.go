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
	MicrosoftAuthURL     = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
	MicrosoftTokenURL    = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	MicrosoftUserInfoURL = "https://graph.microsoft.com/v1.0/me"
)

// MicrosoftOAuthService implementación del servicio OAuth para Microsoft
type MicrosoftOAuthService struct {
	config       OAuthConfig
	httpClient   *http.Client
	stateManager StateManager
}

// NewMicrosoftOAuthService crea una nueva instancia del servicio Microsoft OAuth
func NewMicrosoftOAuthService(config OAuthConfig, stateManager StateManager) *MicrosoftOAuthService {
	if len(config.Scopes) == 0 {
		config.Scopes = []string{"openid", "email", "profile", "User.Read"}
	}

	return &MicrosoftOAuthService{
		config:       config,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		stateManager: stateManager,
	}
}

// GetProvider retorna el proveedor OAuth
func (m *MicrosoftOAuthService) GetProvider() iam.OAuthProvider {
	return iam.OAuthProviderMicrosoft
}

// GetAuthURL genera la URL de autorización de Microsoft
func (m *MicrosoftOAuthService) GetAuthURL(state string) string {
	params := url.Values{
		"client_id":     {m.config.ClientID},
		"redirect_uri":  {m.config.RedirectURL},
		"scope":         {strings.Join(m.config.Scopes, " ")},
		"response_type": {"code"},
		"state":         {state},
		"response_mode": {"query"},
	}

	return fmt.Sprintf("%s?%s", MicrosoftAuthURL, params.Encode())
}

// ValidateState valida el estado OAuth
func (m *MicrosoftOAuthService) ValidateState(state string) bool {
	return m.stateManager.ValidateState(state)
}

// ExchangeToken intercambia el código de autorización por tokens
func (m *MicrosoftOAuthService) ExchangeToken(ctx context.Context, code string) (*OAuthTokenResponse, error) {
	data := url.Values{
		"client_id":     {m.config.ClientID},
		"client_secret": {m.config.ClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {m.config.RedirectURL},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", MicrosoftTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, errx.Wrap(err, "failed to create token request", errx.TypeInternal)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, errx.Wrap(err, "failed to exchange token", errx.TypeExternal)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrOAuthAuthorizationFailed().
			WithDetail("status_code", resp.StatusCode).
			WithDetail("provider", "microsoft")
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, errx.Wrap(err, "failed to decode token response", errx.TypeExternal)
	}

	return &tokenResp, nil
}

// GetUserInfo obtiene la información del usuario desde Microsoft
func (m *MicrosoftOAuthService) GetUserInfo(ctx context.Context, accessToken string) (*OAuthUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", MicrosoftUserInfoURL, nil)
	if err != nil {
		return nil, errx.Wrap(err, "failed to create user info request", errx.TypeInternal)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get user info", errx.TypeExternal)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrOAuthAuthorizationFailed().
			WithDetail("status_code", resp.StatusCode).
			WithDetail("provider", "microsoft").
			WithDetail("endpoint", "userinfo")
	}

	var msUser struct {
		ID                string `json:"id"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&msUser); err != nil {
		return nil, errx.Wrap(err, "failed to decode user info", errx.TypeExternal)
	}

	// Microsoft puede usar mail o userPrincipalName como email
	email := msUser.Mail
	if email == "" {
		email = msUser.UserPrincipalName
	}

	return &OAuthUserInfo{
		ID:            msUser.ID,
		Email:         email,
		Name:          msUser.DisplayName,
		Picture:       "",   // Microsoft Graph requiere endpoint separado para foto
		EmailVerified: true, // Asumimos verificado si viene de Microsoft
	}, nil
}

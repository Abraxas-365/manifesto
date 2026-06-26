package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam"
)

const (
	MicrosoftAuthURL     = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
	MicrosoftTokenURL    = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	MicrosoftUserInfoURL = "https://graph.microsoft.com/v1.0/me"
)

// MicrosoftOAuthService is the OAuth service implementation for Microsoft
type MicrosoftOAuthService struct {
	config       OAuthConfig
	httpClient   *http.Client
	stateManager StateManager
	authURL      string
	tokenURL     string
	userInfoURL  string
}

// NewMicrosoftOAuthServiceFromConfig creates a new Microsoft OAuth service instance
func NewMicrosoftOAuthServiceFromConfig(cfg *config.OAuthProviderConfig, stateManager StateManager) *MicrosoftOAuthService {
	return &MicrosoftOAuthService{
		config: OAuthConfig{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
		httpClient:   &http.Client{Timeout: cfg.Timeout},
		stateManager: stateManager,
		authURL:      cfg.AuthURL,
		tokenURL:     cfg.TokenURL,
		userInfoURL:  cfg.UserInfoURL,
	}
}

// GetProvider returns the OAuth provider
func (m *MicrosoftOAuthService) GetProvider() iam.OAuthProvider {
	return iam.OAuthProviderMicrosoft
}

// GetAuthURL generates the Microsoft authorization URL
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

// ValidateState validates the OAuth state
func (m *MicrosoftOAuthService) ValidateState(state string) bool {
	return m.stateManager.ValidateState(state)
}

// ExchangeToken exchanges the authorization code for tokens
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

// GetUserInfo retrieves user information from Microsoft
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

	// Microsoft may use mail or userPrincipalName as email
	email := msUser.Mail
	if email == "" {
		email = msUser.UserPrincipalName
	}

	return &OAuthUserInfo{
		ID:            msUser.ID,
		Email:         email,
		Name:          msUser.DisplayName,
		Picture:       "",   // Microsoft Graph requires a separate endpoint for photos
		EmailVerified: true, // Assumed verified if coming from Microsoft
	}, nil
}

package auth

import (
	"context"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/role"

	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/Abraxas-365/manifesto/internal/ptrx"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AuthHandlers handles authentication routes with Fiber
type AuthHandlers struct {
	oauthServices  map[iam.OAuthProvider]OAuthService
	tokenService   TokenService
	userRepo       user.UserRepository
	tenantRepo     tenant.TenantRepository
	tokenRepo      TokenRepository
	sessionRepo    SessionRepository
	stateManager   StateManager
	invitationRepo invitation.InvitationRepository
	roleRepo       role.RoleRepository
	auditService   AuditService
	scopeResolver  ScopeResolver
	config         *config.Config
}

// NewAuthHandlers creates a new authentication handler
func NewAuthHandlers(
	oauthServices map[iam.OAuthProvider]OAuthService,
	tokenService TokenService,
	userRepo user.UserRepository,
	tenantRepo tenant.TenantRepository,
	tokenRepo TokenRepository,
	sessionRepo SessionRepository,
	stateManager StateManager,
	invitationRepo invitation.InvitationRepository,
	roleRepo role.RoleRepository,
	auditService AuditService,
	scopeResolver ScopeResolver,
	config *config.Config,
) *AuthHandlers {
	return &AuthHandlers{
		oauthServices:  oauthServices,
		tokenService:   tokenService,
		userRepo:       userRepo,
		tenantRepo:     tenantRepo,
		tokenRepo:      tokenRepo,
		sessionRepo:    sessionRepo,
		stateManager:   stateManager,
		invitationRepo: invitationRepo,
		roleRepo:       roleRepo,
		auditService:   auditService,
		scopeResolver:  scopeResolver,
		config:         config,
	}
}

// LoginRequest is the request to initiate OAuth login
type LoginRequest struct {
	Provider        iam.OAuthProvider `json:"provider"`
	InvitationToken string            `json:"invitation_token,omitempty"`
}

func (r *LoginRequest) Validate() error {
	if strings.TrimSpace(string(r.Provider)) == "" {
		return errx.Validation("provider is required").WithDetail("field", "provider")
	}
	return nil
}

// LoginResponse is the login endpoint response
type LoginResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// TokenResponse is the response with authentication tokens
type TokenResponse struct {
	AccessToken  string                  `json:"access_token"`
	RefreshToken string                  `json:"refresh_token"`
	TokenType    string                  `json:"token_type"`
	ExpiresIn    int                     `json:"expires_in"`
	User         user.UserDetailsDTO     `json:"user"`
	Tenant       tenant.TenantDetailsDTO `json:"tenant"`
}

// RefreshTokenRequest is the request to refresh a token
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (r *RefreshTokenRequest) Validate() error {
	if strings.TrimSpace(r.RefreshToken) == "" {
		return errx.Validation("refresh_token is required").WithDetail("field", "refresh_token")
	}
	return nil
}

// RegisterRoutes registers the auth routes on Fiber
func (ah *AuthHandlers) RegisterRoutes(router fiber.Router) {
	auth := router.Group("/auth")

	auth.Post("/login", ah.InitiateLogin)
	auth.Get("/callback/:provider", ah.HandleCallback)
	auth.Post("/refresh", ah.RefreshToken)
	auth.Post("/logout", ah.Logout)
	auth.Post("/logout/all", ah.LogoutAll)
	auth.Get("/me", ah.GetCurrentUser)
	auth.Get("/sessions", ah.ListSessions)
}

// InitiateLogin starts the OAuth login process
func (ah *AuthHandlers) InitiateLogin(c *fiber.Ctx) error {
	req, err := kernel.BindAndValidate[LoginRequest](c)
	if err != nil {
		return err
	}

	// Normalize the provider to uppercase and verify it is supported
	normalizedProvider := iam.OAuthProvider(strings.ToUpper(string(req.Provider)))
	oauthService, exists := ah.oauthServices[normalizedProvider]
	if !exists {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": ErrInvalidOAuthProvider().Error(),
		})
	}

	// Generate OAuth state
	state := ah.stateManager.GenerateState()

	// Store state information
	stateData := map[string]interface{}{
		"provider": normalizedProvider,
	}
	if req.InvitationToken != "" {
		stateData["invitation_token"] = req.InvitationToken
	}

	if err := ah.stateManager.StoreState(c.Context(), state, stateData); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to store OAuth state",
		})
	}

	// Generate authorization URL
	authURL := oauthService.GetAuthURL(state)

	return c.JSON(LoginResponse{
		AuthURL: authURL,
		State:   state,
	})
}

// HandleCallback handles the OAuth callback
func (ah *AuthHandlers) HandleCallback(c *fiber.Ctx) error {
	providerStr := c.Params("provider")

	// Convert string to OAuthProvider
	var provider iam.OAuthProvider
	switch providerStr {
	case "google":
		provider = iam.OAuthProviderGoogle
	case "microsoft":
		provider = iam.OAuthProviderMicrosoft
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": ErrInvalidOAuthProvider().Error(),
		})
	}

	// Verify the OAuth service exists
	oauthService, exists := ah.oauthServices[provider]
	if !exists {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": ErrInvalidOAuthProvider().Error(),
		})
	}

	// Get callback parameters
	code := c.Query("code")
	state := c.Query("state")
	errorParam := c.Query("error")

	// Check for OAuth errors
	if errorParam != "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": ErrOAuthCallbackError().WithDetail("error", errorParam).Error(),
		})
	}

	if code == "" || state == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Missing code or state parameter",
		})
	}

	// Validate state
	stateData, err := ah.stateManager.GetStateData(c.Context(), state)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": ErrInvalidState().Error(),
		})
	}

	// Exchange code for token
	tokenResp, err := oauthService.ExchangeToken(c.Context(), code)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Get user information
	userInfo, err := oauthService.GetUserInfo(c.Context(), tokenResp.AccessToken)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Find or create user
	userEntity, tenantEntity, err := ah.findOrCreateUser(c.Context(), userInfo, provider, stateData, c.IP())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Generate application tokens and create session
	response, err := ah.createSessionAndTokens(c, userEntity, tenantEntity)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Update user's last login
	userEntity.UpdateLastLogin()
	if err := ah.userRepo.Save(c.Context(), *userEntity); err != nil {
		// Log error but don't fail
	}

	// Audit: successful OAuth login
	ah.auditService.LogLoginAttempt(c.Context(), userEntity.ID, tenantEntity.ID, "oauth_"+strings.ToLower(string(provider)), true, c.IP(), c.Get("User-Agent"))

	return c.JSON(response)
}

// RefreshToken renews an access token using a refresh token.
// Implements refresh token rotation: the old token is revoked and a new one is issued.
// If a revoked token is presented, it indicates theft — the entire session is revoked.
func (ah *AuthHandlers) RefreshToken(c *fiber.Ctx) error {
	req, err := kernel.BindAndValidate[RefreshTokenRequest](c)
	if err != nil {
		// Fall back to cookie-based refresh token before failing validation
		if cookieToken := c.Cookies(ah.config.Auth.Cookie.RefreshTokenName); cookieToken != "" {
			req.RefreshToken = cookieToken
		} else {
			return err
		}
	}

	// Find refresh token in database (includes revoked tokens for theft detection)
	refreshToken, err := ah.tokenRepo.FindRefreshToken(c.Context(), req.RefreshToken)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": ErrInvalidRefreshToken().Error(),
		})
	}

	// Theft detection: if the token was already revoked, someone stole it.
	// Revoke the entire session to protect the user.
	if refreshToken.IsRevoked {
		if refreshToken.SessionID != "" {
			ah.tokenRepo.RevokeRefreshTokensBySessionID(c.Context(), refreshToken.SessionID)
			ah.sessionRepo.RevokeSession(c.Context(), refreshToken.SessionID)
		}
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": ErrInvalidRefreshToken().Error(),
		})
	}

	// Verify refresh token validity (expiration)
	if refreshToken.IsExpired() {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": ErrExpiredRefreshToken().Error(),
		})
	}

	// Verify the session is still active
	if refreshToken.SessionID != "" {
		session, err := ah.sessionRepo.FindSession(c.Context(), refreshToken.SessionID)
		if err != nil || session.IsExpired() {
			ah.tokenRepo.RevokeRefreshToken(c.Context(), req.RefreshToken)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Session expired or revoked",
			})
		}
		// Update session activity
		ah.sessionRepo.UpdateSessionActivity(c.Context(), refreshToken.SessionID)
	}

	// Find user and tenant
	userEntity, err := ah.userRepo.FindByID(c.Context(), refreshToken.UserID, refreshToken.TenantID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	tenantEntity, err := ah.tenantRepo.FindByID(c.Context(), refreshToken.TenantID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Tenant not found",
		})
	}

	// Verify the user can log in
	if !userEntity.CanLogin() {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "User cannot login",
		})
	}

	// Verify the tenant is active
	if !tenantEntity.IsActive() {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Tenant is not active",
		})
	}

	// --- Rotate refresh token ---
	// 1. Revoke the old refresh token
	ah.tokenRepo.RevokeRefreshToken(c.Context(), req.RefreshToken)

	// 2. Generate new access token with session_id
	effectiveScopes := ah.resolveScopes(c.Context(), userEntity)
	accessToken, err := ah.tokenService.GenerateAccessToken(userEntity.ID, tenantEntity.ID, map[string]any{
		"email":      userEntity.Email,
		"name":       userEntity.Name,
		"scopes":     effectiveScopes,
		"session_id": refreshToken.SessionID,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// 3. Generate new refresh token
	newRefreshTokenStr, err := ah.tokenService.GenerateRefreshToken(userEntity.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// 4. Save new refresh token linked to same session
	newRefreshToken := RefreshToken{
		ID:        generateID(),
		Token:     newRefreshTokenStr,
		UserID:    userEntity.ID,
		TenantID:  tenantEntity.ID,
		SessionID: refreshToken.SessionID,
		ExpiresAt: time.Now().UTC().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}
	if err := ah.tokenRepo.SaveRefreshToken(c.Context(), newRefreshToken); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save refresh token",
		})
	}

	// Audit: token refresh
	ah.auditService.LogTokenRefresh(c.Context(), userEntity.ID, tenantEntity.ID, c.IP())

	// Update cookies
	c.Cookie(&fiber.Cookie{
		Name:     ah.config.Auth.Cookie.AccessTokenName,
		Value:    accessToken,
		Expires:  time.Now().Add(ah.config.Auth.JWT.AccessTokenTTL),
		HTTPOnly: ah.config.Auth.Cookie.HTTPOnly,
		Secure:   ah.config.Auth.Cookie.Secure,
		SameSite: ah.config.Auth.Cookie.SameSite,
		Domain:   ah.config.Auth.Cookie.Domain,
		Path:     ah.config.Auth.Cookie.Path,
	})

	c.Cookie(&fiber.Cookie{
		Name:     ah.config.Auth.Cookie.RefreshTokenName,
		Value:    newRefreshTokenStr,
		Expires:  time.Now().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		HTTPOnly: ah.config.Auth.Cookie.HTTPOnly,
		Secure:   ah.config.Auth.Cookie.Secure,
		SameSite: ah.config.Auth.Cookie.SameSite,
		Domain:   ah.config.Auth.Cookie.Domain,
		Path:     ah.config.Auth.Cookie.Path,
	})

	return c.JSON(fiber.Map{
		"access_token":  accessToken,
		"refresh_token": newRefreshTokenStr,
		"token_type":    "Bearer",
		"expires_in":    int(ah.config.Auth.JWT.AccessTokenTTL / time.Second),
	})
}

// Logout invalidates the current session and its tokens (single-device logout)
func (ah *AuthHandlers) Logout(c *fiber.Ctx) error {
	authContext, ok := GetAuthContext(c)
	if !ok || authContext.UserID == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": iam.ErrUnauthorized().Error(),
		})
	}

	// Revoke current session and its tokens
	if authContext.SessionID != "" {
		ah.tokenRepo.RevokeRefreshTokensBySessionID(c.Context(), authContext.SessionID)
		ah.sessionRepo.RevokeSession(c.Context(), authContext.SessionID)
	}

	// Audit: logout
	ah.auditService.LogLogout(c.Context(), *authContext.UserID, authContext.TenantID, c.IP())

	// Clear cookies
	ah.clearAuthCookies(c)

	return c.JSON(fiber.Map{
		"message": "Logged out successfully",
	})
}

// LogoutAll invalidates all user sessions and tokens across all devices
func (ah *AuthHandlers) LogoutAll(c *fiber.Ctx) error {
	authContext, ok := GetAuthContext(c)
	if !ok || authContext.UserID == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": iam.ErrUnauthorized().Error(),
		})
	}

	// Revoke all refresh tokens
	ah.tokenRepo.RevokeAllUserTokens(c.Context(), *authContext.UserID)

	// Revoke all sessions
	ah.sessionRepo.RevokeAllUserSessions(c.Context(), *authContext.UserID)

	// Audit: logout
	ah.auditService.LogLogout(c.Context(), *authContext.UserID, authContext.TenantID, c.IP())

	// Clear cookies
	ah.clearAuthCookies(c)

	return c.JSON(fiber.Map{
		"message": "All sessions logged out successfully",
	})
}

// ListSessions returns all active sessions for the current user
func (ah *AuthHandlers) ListSessions(c *fiber.Ctx) error {
	authContext, ok := GetAuthContext(c)
	if !ok || authContext.UserID == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": iam.ErrUnauthorized().Error(),
		})
	}

	sessions, err := ah.sessionRepo.FindUserSessions(c.Context(), *authContext.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to list sessions",
		})
	}

	type sessionDTO struct {
		ID           string    `json:"id"`
		IPAddress    string    `json:"ip_address"`
		UserAgent    string    `json:"user_agent"`
		CreatedAt    time.Time `json:"created_at"`
		LastActivity time.Time `json:"last_activity"`
		Current      bool      `json:"current"`
	}

	dtos := make([]sessionDTO, len(sessions))
	for i, s := range sessions {
		dtos[i] = sessionDTO{
			ID:           s.ID,
			IPAddress:    s.IPAddress,
			UserAgent:    s.UserAgent,
			CreatedAt:    s.CreatedAt,
			LastActivity: s.LastActivity,
			Current:      s.ID == authContext.SessionID,
		}
	}

	return c.JSON(fiber.Map{
		"sessions": dtos,
		"count":    len(dtos),
	})
}

// clearAuthCookies clears access and refresh token cookies
func (ah *AuthHandlers) clearAuthCookies(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     ah.config.Auth.Cookie.AccessTokenName,
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: ah.config.Auth.Cookie.HTTPOnly,
		Secure:   ah.config.Auth.Cookie.Secure,
		SameSite: ah.config.Auth.Cookie.SameSite,
		Domain:   ah.config.Auth.Cookie.Domain,
		Path:     ah.config.Auth.Cookie.Path,
	})

	c.Cookie(&fiber.Cookie{
		Name:     ah.config.Auth.Cookie.RefreshTokenName,
		Value:    "",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: ah.config.Auth.Cookie.HTTPOnly,
		Secure:   ah.config.Auth.Cookie.Secure,
		SameSite: ah.config.Auth.Cookie.SameSite,
		Domain:   ah.config.Auth.Cookie.Domain,
		Path:     ah.config.Auth.Cookie.Path,
	})
}

// GetCurrentUser retrieves the authenticated user's information
func (ah *AuthHandlers) GetCurrentUser(c *fiber.Ctx) error {
	authContext, ok := GetAuthContext(c)
	if !ok || authContext.UserID == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": iam.ErrUnauthorized().Error(),
		})
	}

	// Find complete user
	userEntity, err := ah.userRepo.FindByID(c.Context(), *authContext.UserID, authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	// Find tenant
	tenantEntity, err := ah.tenantRepo.FindByID(c.Context(), authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Tenant not found",
		})
	}

	return c.JSON(fiber.Map{
		"user":   userEntity.ToDTO(),
		"tenant": tenantEntity.ToDTO(),
	})
}

// findOrCreateUser handles user lookup, creation, and account linking for OAuth
func (ah *AuthHandlers) findOrCreateUser(ctx context.Context, userInfo *OAuthUserInfo, provider iam.OAuthProvider, stateData map[string]interface{}, ip string) (*user.User, *tenant.Tenant, error) {
	var tenantEntity *tenant.Tenant
	var invitationToken string
	var invitationScopes []string
	var invitationRoleID *kernel.RoleID
	var err error

	// Check if there's an invitation token
	if token, ok := stateData["invitation_token"].(string); ok && token != "" {
		invitationToken = token
	}

	// If there's an invitation token, validate it and get the tenant
	if invitationToken != "" {
		inv, err := ah.invitationRepo.FindByToken(ctx, invitationToken)
		if err != nil {
			return nil, nil, errx.New("invalid invitation token", errx.TypeBusiness)
		}

		if !inv.CanBeAccepted() {
			if inv.IsExpired() {
				return nil, nil, errx.New("invitation expired", errx.TypeBusiness)
			}
			return nil, nil, errx.New("invitation not valid", errx.TypeBusiness)
		}

		if inv.GetEmail() != userInfo.Email {
			return nil, nil, errx.New("email does not match invitation", errx.TypeBusiness)
		}

		invitationScopes = inv.GetScopes()
		invitationRoleID = inv.GetRoleID()

		tenantEntity, err = ah.tenantRepo.FindByID(ctx, inv.GetTenantID())
		if err != nil {
			return nil, nil, tenant.ErrTenantNotFound()
		}
	} else {
		return nil, nil, errx.New("invitation required for registration", errx.TypeAuthorization)
	}

	// Account linking: look up existing user
	existingUser, err := ah.userRepo.FindByEmail(ctx, userInfo.Email, tenantEntity.ID)
	if err == nil {
		needsSave := false

		if existingUser.OAuthProvider != provider || existingUser.OAuthProviderID != userInfo.ID {
			existingUser.LinkOAuth(provider, userInfo.ID)
			existingUser.UpdateProfile(userInfo.Name, userInfo.Picture)
			needsSave = true
			ah.auditService.LogAccountLinked(ctx, existingUser.ID, tenantEntity.ID, "oauth_"+strings.ToLower(string(provider)), ip)
		}

		// Apply invitation scopes to existing user
		for _, scope := range invitationScopes {
			if !existingUser.HasScope(scope) {
				existingUser.AddScope(scope)
				needsSave = true
			}
		}

		if needsSave {
			if err := ah.userRepo.Save(ctx, *existingUser); err != nil {
				return nil, nil, err
			}
		}

		// Assign role from invitation
		ah.assignInvitationRole(ctx, existingUser.ID, tenantEntity.ID, invitationRoleID)

		// Accept invitation for account linking
		if invitationToken != "" {
			inv, err := ah.invitationRepo.FindByToken(ctx, invitationToken)
			if err == nil {
				if err := inv.Accept(existingUser.ID); err == nil {
					ah.invitationRepo.Save(ctx, *inv)
				}
			}
		}

		return existingUser, tenantEntity, nil
	}

	// Check if the tenant can add more users
	if !tenantEntity.CanAddUser() {
		return nil, nil, tenant.ErrMaxUsersReached()
	}

	// Determine scopes
	var userScopes []string
	if len(invitationScopes) > 0 {
		userScopes = invitationScopes
	} else {
		userScopes = []string{}
	}

	// Create new user with OAuth (OTPEnabled = false by default)
	newUser := &user.User{
		ID:              kernel.NewUserID(generateID()),
		TenantID:        tenantEntity.ID,
		Email:           userInfo.Email,
		Name:            userInfo.Name,
		Picture:         ptrx.String(userInfo.Picture),
		Status:          user.UserStatusActive,
		Scopes:          userScopes,
		OAuthProvider:   provider,
		OAuthProviderID: userInfo.ID,
		OTPEnabled:      false, // 🔥 OAuth users don't have OTP by default
		EmailVerified:   userInfo.EmailVerified,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Save user
	if err := ah.userRepo.Save(ctx, *newUser); err != nil {
		return nil, nil, err
	}

	// Increment tenant user count
	if err := tenantEntity.AddUser(); err != nil {
		ah.userRepo.Delete(ctx, newUser.ID, tenantEntity.ID)
		return nil, nil, err
	}

	// Save updated tenant
	if err := ah.tenantRepo.Save(ctx, *tenantEntity); err != nil {
		// Log error but don't fail
	}

	// Audit: account created
	ah.auditService.LogAccountCreated(ctx, newUser.ID, tenantEntity.ID, "oauth_"+strings.ToLower(string(provider)), ip)

	// Assign role from invitation
	ah.assignInvitationRole(ctx, newUser.ID, tenantEntity.ID, invitationRoleID)

	// Accept the invitation
	if invitationToken != "" {
		inv, err := ah.invitationRepo.FindByToken(ctx, invitationToken)
		if err == nil {
			if err := inv.Accept(newUser.ID); err == nil {
				ah.invitationRepo.Save(ctx, *inv)
			}
		}
	}

	return newUser, tenantEntity, nil
}

func (ah *AuthHandlers) resolveScopes(ctx context.Context, userEntity *user.User) []string {
	return ResolveScopes(ctx, ah.scopeResolver, userEntity.ID, userEntity.TenantID, userEntity.Scopes)
}

// assignInvitationRole assigns the invitation's role to the user if present
func (ah *AuthHandlers) assignInvitationRole(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, roleID *kernel.RoleID) {
	if roleID == nil || roleID.IsEmpty() {
		return
	}
	userRole := role.UserRole{
		UserID:     userID,
		RoleID:     *roleID,
		TenantID:   tenantID,
		AssignedAt: time.Now().UTC(),
	}
	ah.roleRepo.AssignToUser(ctx, userRole)
}

// createSessionAndTokens creates a session, enforces max sessions, generates tokens,
// and links the refresh token to the session. Shared by OAuth and passwordless flows.
func (ah *AuthHandlers) createSessionAndTokens(c *fiber.Ctx, userEntity *user.User, tenantEntity *tenant.Tenant) (*TokenResponse, error) {
	ctx := c.Context()
	sessionID := generateID()

	// Enforce max sessions: evict oldest if at limit
	count, err := ah.sessionRepo.CountActiveSessions(ctx, userEntity.ID)
	if err == nil && count >= ah.config.Auth.Session.MaxSessions {
		oldest, err := ah.sessionRepo.FindOldestSession(ctx, userEntity.ID)
		if err == nil {
			ah.tokenRepo.RevokeRefreshTokensBySessionID(ctx, oldest.ID)
			ah.sessionRepo.RevokeSession(ctx, oldest.ID)
		}
	}

	// Create session
	session := UserSession{
		ID:           sessionID,
		UserID:       userEntity.ID,
		TenantID:     tenantEntity.ID,
		IPAddress:    c.IP(),
		UserAgent:    c.Get("User-Agent"),
		ExpiresAt:    time.Now().UTC().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
	if err := ah.sessionRepo.SaveSession(ctx, session); err != nil {
		return nil, err
	}

	// Generate JWT with session_id embedded
	effectiveScopes := ah.resolveScopes(ctx, userEntity)
	accessToken, err := ah.tokenService.GenerateAccessToken(userEntity.ID, tenantEntity.ID, map[string]any{
		"email":      userEntity.Email,
		"name":       userEntity.Name,
		"scopes":     effectiveScopes,
		"session_id": sessionID,
	})
	if err != nil {
		return nil, err
	}

	refreshTokenStr, err := ah.tokenService.GenerateRefreshToken(userEntity.ID)
	if err != nil {
		return nil, err
	}

	// Save refresh token linked to session
	refreshToken := RefreshToken{
		ID:        generateID(),
		Token:     refreshTokenStr,
		UserID:    userEntity.ID,
		TenantID:  tenantEntity.ID,
		SessionID: sessionID,
		ExpiresAt: time.Now().UTC().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}
	if err := ah.tokenRepo.SaveRefreshToken(ctx, refreshToken); err != nil {
		return nil, err
	}

	// Set cookies
	c.Cookie(&fiber.Cookie{
		Name:     ah.config.Auth.Cookie.AccessTokenName,
		Value:    accessToken,
		Expires:  time.Now().Add(ah.config.Auth.JWT.AccessTokenTTL),
		HTTPOnly: ah.config.Auth.Cookie.HTTPOnly,
		Secure:   ah.config.Auth.Cookie.Secure,
		SameSite: ah.config.Auth.Cookie.SameSite,
		Domain:   ah.config.Auth.Cookie.Domain,
		Path:     ah.config.Auth.Cookie.Path,
	})

	c.Cookie(&fiber.Cookie{
		Name:     ah.config.Auth.Cookie.RefreshTokenName,
		Value:    refreshTokenStr,
		Expires:  time.Now().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		HTTPOnly: ah.config.Auth.Cookie.HTTPOnly,
		Secure:   ah.config.Auth.Cookie.Secure,
		SameSite: ah.config.Auth.Cookie.SameSite,
		Domain:   ah.config.Auth.Cookie.Domain,
		Path:     ah.config.Auth.Cookie.Path,
	})

	return &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
		TokenType:    "Bearer",
		ExpiresIn:    int(ah.config.Auth.JWT.AccessTokenTTL / time.Second),
		User:         userEntity.ToDTO(),
		Tenant:       tenantEntity.ToDTO(),
	}, nil
}

// Helper functions
func generateID() string {
	return uuid.NewString()
}

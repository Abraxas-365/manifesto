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
	auth.Get("/me", ah.GetCurrentUser)
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

	// Generate application tokens
	effectiveScopes := ah.resolveScopes(c.Context(), userEntity)
	accessToken, err := ah.tokenService.GenerateAccessToken(userEntity.ID, tenantEntity.ID, map[string]any{
		"email":  userEntity.Email,
		"name":   userEntity.Name,
		"scopes": effectiveScopes,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	refreshTokenStr, err := ah.tokenService.GenerateRefreshToken(userEntity.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Save refresh token to database
	refreshToken := RefreshToken{
		ID:        generateID(),
		Token:     refreshTokenStr,
		UserID:    userEntity.ID,
		TenantID:  tenantEntity.ID,
		ExpiresAt: time.Now().UTC().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}

	if err := ah.tokenRepo.SaveRefreshToken(c.Context(), refreshToken); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save refresh token",
		})
	}

	// Create user session
	session := UserSession{
		ID:           generateID(),
		UserID:       userEntity.ID,
		TenantID:     tenantEntity.ID,
		SessionToken: generateID(),
		IPAddress:    c.IP(),
		UserAgent:    c.Get("User-Agent"),
		ExpiresAt:    time.Now().UTC().Add(ah.config.Auth.JWT.RefreshTokenTTL),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	if err := ah.sessionRepo.SaveSession(c.Context(), session); err != nil {
		// Log error but don't fail authentication
	}

	// Update user's last login
	userEntity.UpdateLastLogin()
	if err := ah.userRepo.Save(c.Context(), *userEntity); err != nil {
		// Log error but don't fail
	}

	// Audit: successful OAuth login
	ah.auditService.LogLoginAttempt(c.Context(), userEntity.ID, tenantEntity.ID, "oauth_"+strings.ToLower(string(provider)), true, c.IP(), c.Get("User-Agent"))

	response := TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
		TokenType:    "Bearer",
		ExpiresIn:    int(ah.config.Auth.JWT.AccessTokenTTL / time.Second),
		User:         userEntity.ToDTO(),
		Tenant:       tenantEntity.ToDTO(),
	}

	// Set cookies for browser-based apps
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

	return c.JSON(response)
}

// RefreshToken renews an access token using a refresh token
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

	// Find refresh token in database
	refreshToken, err := ah.tokenRepo.FindRefreshToken(c.Context(), req.RefreshToken)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": ErrInvalidRefreshToken().Error(),
		})
	}

	// Verify refresh token validity
	if !refreshToken.IsValid() {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": ErrExpiredRefreshToken().Error(),
		})
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

	// Generate new access token
	effectiveScopes := ah.resolveScopes(c.Context(), userEntity)
	accessToken, err := ah.tokenService.GenerateAccessToken(userEntity.ID, tenantEntity.ID, map[string]any{
		"email":  userEntity.Email,
		"name":   userEntity.Name,
		"scopes": effectiveScopes,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Audit: token refresh
	ah.auditService.LogTokenRefresh(c.Context(), userEntity.ID, tenantEntity.ID, c.IP())

	// Update access token cookie
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

	return c.JSON(fiber.Map{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(ah.config.Auth.JWT.AccessTokenTTL / time.Second),
	})
}

// Logout invalidates user tokens and sessions
func (ah *AuthHandlers) Logout(c *fiber.Ctx) error {
	// Try to get auth context from middleware
	authContext, ok := GetAuthContext(c)
	if !ok {
		// Fallback: try to decode the token
		var token string
		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "" {
				token = parts[1]
			}
		}
		if token == "" {
			token = c.Cookies(ah.config.Auth.Cookie.AccessTokenName)
		}
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": iam.ErrUnauthorized().Error(),
			})
		}
		claims, err := ah.tokenService.ValidateAccessToken(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": iam.ErrUnauthorized().Error(),
			})
		}
		authContext = &kernel.AuthContext{
			UserID:   &claims.UserID,
			TenantID: claims.TenantID,
			Email:    claims.Email,
			Name:     claims.Name,
			Scopes:   claims.Scopes,
			IsAPIKey: false,
		}
	}

	if authContext.UserID == nil {
		return iam.ErrUnauthorized()
	}

	// Revoke all refresh tokens
	if err := ah.tokenRepo.RevokeAllUserTokens(c.Context(), *authContext.UserID); err != nil {
		// Log error but don't fail
	}

	// Revoke all sessions
	if err := ah.sessionRepo.RevokeAllUserSessions(c.Context(), *authContext.UserID); err != nil {
		// Log error but don't fail
	}

	// Audit: logout
	ah.auditService.LogLogout(c.Context(), *authContext.UserID, authContext.TenantID, c.IP())

	// Clear cookies
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

	return c.JSON(fiber.Map{
		"message": "Logged out successfully",
	})
}

// GetCurrentUser retrieves the authenticated user's information
func (ah *AuthHandlers) GetCurrentUser(c *fiber.Ctx) error {
	authContext, ok := GetAuthContext(c)
	if !ok {
		// Fallback: try to decode the token
		var token string
		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "" {
				token = parts[1]
			}
		}
		if token == "" {
			token = c.Cookies(ah.config.Auth.Cookie.AccessTokenName)
		}
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": iam.ErrUnauthorized().Error(),
			})
		}
		claims, err := ah.tokenService.ValidateAccessToken(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": iam.ErrUnauthorized().Error(),
			})
		}
		authContext = &kernel.AuthContext{
			UserID:   &claims.UserID,
			TenantID: claims.TenantID,
			Email:    claims.Email,
			Name:     claims.Name,
			Scopes:   claims.Scopes,
			IsAPIKey: false,
		}
	}

	if authContext.UserID == nil {
		return iam.ErrUnauthorized()
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
	var invitationRoleID *string
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
func (ah *AuthHandlers) assignInvitationRole(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, roleID *string) {
	if roleID == nil || *roleID == "" {
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

// Helper functions
func generateID() string {
	return uuid.NewString()
}

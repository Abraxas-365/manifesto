package auth

import (
	"fmt"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/iam"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp/otpsrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant"
	"github.com/Abraxas-365/manifesto/pkg/iam/user"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// PasswordlessAuthHandlers handles OTP-based passwordless authentication
type PasswordlessAuthHandlers struct {
	tokenService   TokenService
	userRepo       user.UserRepository
	tenantRepo     tenant.TenantRepository
	tokenRepo      TokenRepository
	sessionRepo    SessionRepository
	invitationRepo invitation.InvitationRepository
	otpService     *otpsrv.OTPService
	auditService   AuditService
	config         *config.Config
}

func NewPasswordlessAuthHandlers(
	tokenService TokenService,
	userRepo user.UserRepository,
	tenantRepo tenant.TenantRepository,
	tokenRepo TokenRepository,
	sessionRepo SessionRepository,
	invitationRepo invitation.InvitationRepository,
	otpService *otpsrv.OTPService,
	auditService AuditService,
	config *config.Config,
) *PasswordlessAuthHandlers {
	return &PasswordlessAuthHandlers{
		tokenService:   tokenService,
		userRepo:       userRepo,
		tenantRepo:     tenantRepo,
		tokenRepo:      tokenRepo,
		sessionRepo:    sessionRepo,
		invitationRepo: invitationRepo,
		otpService:     otpService,
		auditService:   auditService,
		config:         config,
	}
}

// RegisterRoutes registers passwordless auth routes
func (h *PasswordlessAuthHandlers) RegisterRoutes(router fiber.Router) {
	auth := router.Group("/auth/passwordless")

	// Tenant lookup (public - before login)
	auth.Post("/tenants", h.GetUserTenants)

	// Signup flow
	auth.Post("/signup/initiate", h.InitiateSignup)
	auth.Post("/signup/verify", h.VerifySignup)

	// Login flow
	auth.Post("/login/initiate", h.InitiateLogin)
	auth.Post("/login/verify", h.VerifyLogin)

	// Utility
	auth.Post("/resend-otp", h.ResendOTP)
}

// ============================================================================
// TENANT LOOKUP (for multi-tenant user selection)
// ============================================================================

// GetUserTenantsRequest to find tenants for an email
type GetUserTenantsRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// TenantOption represents a tenant the user belongs to
type TenantOption struct {
	TenantID    kernel.TenantID `json:"tenant_id"`
	CompanyName string          `json:"company_name"`
	UserStatus  user.UserStatus `json:"user_status"`
	AuthMethods struct {
		OTP      bool              `json:"otp"`
		OAuth    bool              `json:"oauth"`
		Provider iam.OAuthProvider `json:"oauth_provider,omitempty"`
	} `json:"auth_methods"`
}

type GetUserTenantsResponse struct {
	Email   string         `json:"email"`
	Tenants []TenantOption `json:"tenants"`
	Count   int            `json:"count"`
}

// GetUserTenants returns all tenants where this email has an account
func (h *PasswordlessAuthHandlers) GetUserTenants(c *fiber.Ctx) error {
	var req GetUserTenantsRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Find all users with this email across tenants
	users, err := h.userRepo.FindByEmailAcrossTenants(c.Context(), req.Email)
	if err != nil || len(users) == 0 {
		// Don't reveal if email exists - return empty list
		return c.JSON(GetUserTenantsResponse{
			Email:   req.Email,
			Tenants: []TenantOption{},
			Count:   0,
		})
	}

	// Build tenant options
	tenantOptions := make([]TenantOption, 0, len(users))
	for _, u := range users {
		tenantEntity, err := h.tenantRepo.FindByID(c.Context(), u.TenantID)
		if err != nil || !tenantEntity.IsActive() {
			continue // Skip inactive or deleted tenants
		}

		option := TenantOption{
			TenantID:    u.TenantID,
			CompanyName: tenantEntity.CompanyName,
			UserStatus:  u.Status,
		}

		// Show available auth methods
		option.AuthMethods.OTP = u.HasOTP()
		option.AuthMethods.OAuth = u.HasOAuth()
		option.AuthMethods.Provider = u.OAuthProvider

		tenantOptions = append(tenantOptions, option)
	}

	return c.JSON(GetUserTenantsResponse{
		Email:   req.Email,
		Tenants: tenantOptions,
		Count:   len(tenantOptions),
	})
}

// ============================================================================
// SIGNUP FLOW
// ============================================================================

// InitiateSignupRequest starts the signup process
type InitiateSignupRequest struct {
	Email           string `json:"email" validate:"required,email"`
	Name            string `json:"name" validate:"required,min=2"`
	InvitationToken string `json:"invitation_token" validate:"required"`
}

type InitiateSignupResponse struct {
	Message       string          `json:"message"`
	Email         string          `json:"email"`
	TenantID      kernel.TenantID `json:"tenant_id"`
	RequiresOTP   bool            `json:"requires_otp"`
	ExpiresIn     int             `json:"expires_in_seconds"`
	AccountLinked bool            `json:"account_linked,omitempty"`
	CanLoginWith  *struct {
		OTP      bool              `json:"otp"`
		OAuth    bool              `json:"oauth"`
		Provider iam.OAuthProvider `json:"oauth_provider,omitempty"`
	} `json:"can_login_with,omitempty"`
}

// InitiateSignup creates user account and sends OTP (with account linking support)
func (h *PasswordlessAuthHandlers) InitiateSignup(c *fiber.Ctx) error {
	var req InitiateSignupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// 1. Validate invitation token
	inv, err := h.invitationRepo.FindByToken(c.Context(), req.InvitationToken)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid or expired invitation",
		})
	}

	// 2. Verify invitation is valid
	if !inv.CanBeAccepted() {
		if inv.IsExpired() {
			return c.Status(fiber.StatusGone).JSON(fiber.Map{
				"error": "Invitation has expired",
			})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invitation cannot be accepted",
		})
	}

	// 3. Verify email matches invitation
	if inv.Email != req.Email {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Email does not match invitation",
		})
	}

	tenantID := inv.TenantID

	// 4. Check if tenant is active
	tenantEntity, err := h.tenantRepo.FindByID(c.Context(), tenantID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Tenant not found",
		})
	}
	if !tenantEntity.IsActive() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Tenant is not active",
		})
	}

	// 5. Check if user already exists in this tenant
	existingUser, _ := h.userRepo.FindByEmail(c.Context(), req.Email, tenantID)

	// 🔥 ACCOUNT LINKING: Handle existing user
	if existingUser != nil {
		// User exists - check if we can link OTP authentication
		if existingUser.HasOTP() {
			// Already has OTP enabled
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":                "Account already exists with email/OTP login. Please use login instead.",
				"can_login_with_otp":   true,
				"can_login_with_oauth": existingUser.HasOAuth(),
				"oauth_provider":       existingUser.OAuthProvider,
			})
		}

		// User exists with OAuth only - enable OTP for them
		if existingUser.HasOAuth() {
			existingUser.EnableOTP()

			// Update user to enable OTP
			if err := h.userRepo.Save(c.Context(), *existingUser); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to link OTP to existing account",
				})
			}

			// Audit: OTP linked to existing OAuth account
			h.auditService.LogAccountLinked(c.Context(), existingUser.ID, tenantID, "otp", c.IP())

			// Generate and send OTP
			otpEntity, err := h.otpService.GenerateOTP(c.Context(), req.Email, otp.OTPPurposeVerification)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to send verification code",
				})
			}

			authMethods := struct {
				OTP      bool              `json:"otp"`
				OAuth    bool              `json:"oauth"`
				Provider iam.OAuthProvider `json:"oauth_provider,omitempty"`
			}{
				OTP:      true,
				OAuth:    true,
				Provider: existingUser.OAuthProvider,
			}

			return c.Status(fiber.StatusOK).JSON(InitiateSignupResponse{
				Message:       "OTP authentication linked to your existing account. Please verify your email.",
				Email:         req.Email,
				TenantID:      tenantID,
				RequiresOTP:   true,
				ExpiresIn:     int(time.Until(otpEntity.ExpiresAt).Seconds()),
				AccountLinked: true,
				CanLoginWith:  &authMethods,
			})
		}
	}

	// 6. Check if tenant can add more users (only for new users)
	if existingUser == nil && !tenantEntity.CanAddUser() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Organization has reached maximum user limit",
		})
	}

	// 7. Create NEW user account
	newUser := &user.User{
		ID:            kernel.NewUserID(uuid.NewString()),
		TenantID:      tenantID,
		Email:         req.Email,
		Name:          req.Name,
		Status:        user.UserStatusPending,
		Scopes:        inv.GetScopes(),
		OTPEnabled:    true, // 🔥 Enable OTP for this user
		EmailVerified: false,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// 8. Save user
	if err := h.userRepo.Save(c.Context(), *newUser); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create user account",
		})
	}

	// 9. Update tenant user count
	if err := tenantEntity.AddUser(); err == nil {
		h.tenantRepo.Save(c.Context(), *tenantEntity)
	}

	// Audit: account created via OTP
	h.auditService.LogAccountCreated(c.Context(), newUser.ID, tenantID, "otp", c.IP())

	// 10. Mark invitation as accepted
	if err := inv.Accept(newUser.ID); err == nil {
		h.invitationRepo.Save(c.Context(), *inv)
	}

	// 11. Generate and send OTP
	otpEntity, err := h.otpService.GenerateOTP(c.Context(), req.Email, otp.OTPPurposeVerification)
	if err != nil {
		return c.Status(fiber.StatusPartialContent).JSON(fiber.Map{
			"error":     "Account created but failed to send verification code",
			"message":   "Please request a new code using the resend option",
			"tenant_id": tenantID,
		})
	}

	// 12. Return success response
	return c.Status(fiber.StatusCreated).JSON(InitiateSignupResponse{
		Message:     "Account created! Please check your email for verification code.",
		Email:       req.Email,
		TenantID:    tenantID,
		RequiresOTP: true,
		ExpiresIn:   int(time.Until(otpEntity.ExpiresAt).Seconds()),
	})
}

// VerifySignupRequest completes signup by verifying OTP
type VerifySignupRequest struct {
	Email    string          `json:"email" validate:"required,email"`
	Code     string          `json:"code" validate:"required"`
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
}

// VerifySignup verifies OTP and activates account
func (h *PasswordlessAuthHandlers) VerifySignup(c *fiber.Ctx) error {
	var req VerifySignupRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// 1. Verify OTP
	_, err := h.otpService.VerifyOTP(c.Context(), req.Email, req.Code, otp.OTPPurposeVerification)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// 2. Find user
	userEntity, err := h.userRepo.FindByEmail(c.Context(), req.Email, req.TenantID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	// 3. Activate user if pending
	if userEntity.Status == user.UserStatusPending {
		if err := userEntity.Activate(); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
	}

	userEntity.EmailVerified = true
	userEntity.UpdatedAt = time.Now()

	// 4. Save updated user
	if err := h.userRepo.Save(c.Context(), *userEntity); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to activate account",
		})
	}

	return c.JSON(fiber.Map{
		"message":   "Email verified! Your account is now active. You can log in.",
		"tenant_id": req.TenantID,
		"email":     req.Email,
	})
}

// ============================================================================
// LOGIN FLOW
// ============================================================================

// InitiateLoginRequest starts the login process
type InitiateLoginRequest struct {
	Email    string          `json:"email" validate:"required,email"`
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
}

type InitiateLoginResponse struct {
	Message     string `json:"message"`
	Email       string `json:"email"`
	ExpiresIn   int    `json:"expires_in_seconds"`
	AuthMethods *struct {
		OTP      bool              `json:"otp"`
		OAuth    bool              `json:"oauth"`
		Provider iam.OAuthProvider `json:"oauth_provider,omitempty"`
	} `json:"auth_methods,omitempty"`
}

// InitiateLogin sends OTP for login
func (h *PasswordlessAuthHandlers) InitiateLogin(c *fiber.Ctx) error {
	var req InitiateLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// 1. Find user by email and tenant
	userEntity, err := h.userRepo.FindByEmail(c.Context(), req.Email, req.TenantID)
	if err != nil {
		// Don't reveal if user exists
		return c.JSON(InitiateLoginResponse{
			Message:   "If this email is registered, you'll receive a login code.",
			Email:     req.Email,
			ExpiresIn: 300,
		})
	}

	// 2. Check if user is active
	if !userEntity.IsActive() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Account is not active. Please complete signup verification or contact support.",
		})
	}

	// 🔥 3. Check if OTP is enabled for this user
	if !userEntity.HasOTP() {
		// User signed up with OAuth only - suggest OAuth login
		oauthProvider := "OAuth"
		if userEntity.OAuthProvider == iam.OAuthProviderGoogle {
			oauthProvider = "Google"
		} else if userEntity.OAuthProvider == iam.OAuthProviderMicrosoft {
			oauthProvider = "Microsoft"
		}

		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":                fmt.Sprintf("This account uses %s login. Please sign in with %s instead.", oauthProvider, oauthProvider),
			"can_login_with_oauth": true,
			"oauth_provider":       userEntity.OAuthProvider,
			"suggestion":           "You can enable email/OTP login by signing up again with your invitation link.",
		})
	}

	// 4. Check if email is verified
	if !userEntity.EmailVerified {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":                 "Email not verified. Please verify your email first.",
			"requires_verification": true,
		})
	}

	// 5. Check tenant status
	tenantEntity, err := h.tenantRepo.FindByID(c.Context(), userEntity.TenantID)
	if err != nil || !tenantEntity.IsActive() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Account access is currently unavailable",
		})
	}

	// 6. Generate and send OTP
	otpEntity, err := h.otpService.GenerateOTP(c.Context(), req.Email, otp.OTPPurposeVerification)
	if err != nil {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// 7. Return success response with available auth methods
	authMethods := struct {
		OTP      bool              `json:"otp"`
		OAuth    bool              `json:"oauth"`
		Provider iam.OAuthProvider `json:"oauth_provider,omitempty"`
	}{
		OTP:      true,
		OAuth:    userEntity.HasOAuth(),
		Provider: userEntity.OAuthProvider,
	}

	return c.JSON(InitiateLoginResponse{
		Message:     "Login code sent to your email!",
		Email:       req.Email,
		ExpiresIn:   int(time.Until(otpEntity.ExpiresAt).Seconds()),
		AuthMethods: &authMethods,
	})
}

// VerifyLoginRequest completes login by verifying OTP
type VerifyLoginRequest struct {
	Email    string          `json:"email" validate:"required,email"`
	Code     string          `json:"code" validate:"required"`
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
}

// VerifyLogin verifies OTP and returns JWT tokens
func (h *PasswordlessAuthHandlers) VerifyLogin(c *fiber.Ctx) error {
	var req VerifyLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// 1. Verify OTP
	_, err := h.otpService.VerifyOTP(c.Context(), req.Email, req.Code, otp.OTPPurposeVerification)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid or expired code",
		})
	}

	// 2. Find user
	userEntity, err := h.userRepo.FindByEmail(c.Context(), req.Email, req.TenantID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Authentication failed",
		})
	}

	// 3. Check user can login
	if !userEntity.CanLogin() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Account cannot login. Status: " + string(userEntity.Status),
		})
	}

	// 4. Get tenant
	tenantEntity, err := h.tenantRepo.FindByID(c.Context(), userEntity.TenantID)
	if err != nil || !tenantEntity.IsActive() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Organization is not active",
		})
	}

	// 5. Ensure email is verified
	if !userEntity.EmailVerified {
		userEntity.EmailVerified = true
		h.userRepo.Save(c.Context(), *userEntity)
	}

	// 6. Generate JWT tokens
	accessToken, err := h.tokenService.GenerateAccessToken(userEntity.ID, tenantEntity.ID, map[string]any{
		"email":  userEntity.Email,
		"name":   userEntity.Name,
		"scopes": userEntity.Scopes,
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate access token",
		})
	}

	refreshTokenStr, err := h.tokenService.GenerateRefreshToken(userEntity.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate refresh token",
		})
	}

	// 7. Save refresh token
	refreshToken := RefreshToken{
		ID:       uuid.NewString(),
		Token:    refreshTokenStr,
		UserID:   userEntity.ID,
		TenantID: tenantEntity.ID,

		ExpiresAt: time.Now().UTC().Add(h.config.Auth.JWT.RefreshTokenTTL),

		CreatedAt: time.Now(),
		IsRevoked: false,
	}
	h.tokenRepo.SaveRefreshToken(c.Context(), refreshToken)

	// 8. Create session
	session := UserSession{
		ID:           uuid.NewString(),
		UserID:       userEntity.ID,
		TenantID:     tenantEntity.ID,
		SessionToken: uuid.NewString(),
		IPAddress:    c.IP(),
		UserAgent:    c.Get("User-Agent"),
		ExpiresAt:    time.Now().UTC().Add(h.config.Auth.JWT.RefreshTokenTTL),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
	h.sessionRepo.SaveSession(c.Context(), session)

	// 9. Update last login
	userEntity.UpdateLastLogin()
	h.userRepo.Save(c.Context(), *userEntity)

	// 10. Set cookies
	c.Cookie(&fiber.Cookie{
		Name:     h.config.Auth.Cookie.AccessTokenName,
		Value:    accessToken,
		Expires:  time.Now().Add(h.config.Auth.JWT.AccessTokenTTL),
		HTTPOnly: h.config.Auth.Cookie.HTTPOnly,
		Secure:   h.config.Auth.Cookie.Secure,
		SameSite: h.config.Auth.Cookie.SameSite,
		Domain:   h.config.Auth.Cookie.Domain,
		Path:     h.config.Auth.Cookie.Path,
	})

	c.Cookie(&fiber.Cookie{
		Name:     h.config.Auth.Cookie.RefreshTokenName,
		Value:    refreshTokenStr,
		Expires:  time.Now().Add(h.config.Auth.JWT.RefreshTokenTTL),
		HTTPOnly: h.config.Auth.Cookie.HTTPOnly,
		Secure:   h.config.Auth.Cookie.Secure,
		SameSite: h.config.Auth.Cookie.SameSite,
		Domain:   h.config.Auth.Cookie.Domain,
		Path:     h.config.Auth.Cookie.Path,
	})

	// 11. Audit: successful OTP login
	h.auditService.LogLoginAttempt(c.Context(), userEntity.ID, tenantEntity.ID, "otp", true, c.IP(), c.Get("User-Agent"))

	// 12. Return tokens and user info
	return c.JSON(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
		TokenType:    "Bearer",
		ExpiresIn:    int(h.config.Auth.JWT.AccessTokenTTL / time.Second),
		User:         userEntity.ToDTO(),
		Tenant:       tenantEntity.ToDTO(),
	})
}

// ============================================================================
// UTILITY
// ============================================================================

// ResendOTPRequest for resending OTP
type ResendOTPRequest struct {
	Email    string          `json:"email" validate:"required,email"`
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
	Purpose  string          `json:"purpose" validate:"required,oneof=signup login"`
}

// ResendOTP resends OTP code
func (h *PasswordlessAuthHandlers) ResendOTP(c *fiber.Ctx) error {
	var req ResendOTPRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Verify user exists in the tenant
	userEntity, err := h.userRepo.FindByEmail(c.Context(), req.Email, req.TenantID)
	if err != nil {
		// Don't reveal if user exists
		return c.JSON(fiber.Map{
			"message":    "If this email is registered, a verification code has been sent",
			"expires_in": 300,
		})
	}

	// Check tenant is active
	tenantEntity, err := h.tenantRepo.FindByID(c.Context(), req.TenantID)
	if err != nil || !tenantEntity.IsActive() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Unable to send verification code",
		})
	}

	// Check user status based on purpose
	if req.Purpose == "signup" && userEntity.Status != user.UserStatusPending {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Account is already verified. Please use login instead.",
		})
	}

	if req.Purpose == "login" && !userEntity.IsActive() {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Account is not active",
		})
	}

	// Generate new OTP
	otpEntity, err := h.otpService.GenerateOTP(c.Context(), req.Email, otp.OTPPurposeVerification)
	if err != nil {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message":    "Verification code sent",
		"expires_in": int(time.Until(otpEntity.ExpiresAt).Seconds()),
	})
}

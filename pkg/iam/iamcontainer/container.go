package iamcontainer

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/iam"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey/apikeyapi"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey/apikeyinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey/apikeysrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth/authinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation/invitationapi"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation/invitationinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation/invitationsrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp/otpinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp/otpsrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant/tenantinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant/tenantsrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/user/userinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/user/usersrv"
	"github.com/Abraxas-365/manifesto/pkg/logx"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// Deps: explicit external dependencies this bounded context requires.
// No hidden globals, no ambient state — everything comes through here.
// ---------------------------------------------------------------------------

type Deps struct {
	DB    *sqlx.DB
	Redis *redis.Client
	Cfg   *config.Config

	// OTPNotifier is a cross-context dependency injected as an interface so the
	// IAM module has zero knowledge of the concrete notification implementation.
	OTPNotifier otp.NotificationService
}

// ---------------------------------------------------------------------------
// Container: the public surface of the IAM module.
// Only expose what other modules or cmd/ actually need.
// Internal repos, infra details, etc. stay private.
// ---------------------------------------------------------------------------

type Container struct {
	// Services — available for cross-module consumption via interfaces
	UserService       *usersrv.UserService
	TenantService     *tenantsrv.TenantService
	InvitationService *invitationsrv.InvitationService
	APIKeyService     *apikeysrv.APIKeyService
	OTPService        *otpsrv.OTPService
	TokenService      auth.TokenService

	// Auth handlers — needed by cmd/ to register routes
	OAuthHandlers        *auth.AuthHandlers
	PasswordlessHandlers *auth.PasswordlessAuthHandlers

	// API handlers — needed by cmd/ to register routes
	APIKeyHandlers     *apikeyapi.APIKeyHandlers
	InvitationHandlers *invitationapi.InvitationHandlers

	// Middleware — needed by cmd/ to protect route groups
	AuthMiddleware        *auth.TokenMiddleware
	UnifiedAuthMiddleware *auth.UnifiedAuthMiddleware

	// Background services
	CleanupService *authinfra.CleanupService
}

// ---------------------------------------------------------------------------
// New: constructs the entire IAM dependency graph.
// Order matters: infra → repos → services → handlers → middleware.
// ---------------------------------------------------------------------------

func New(deps Deps) *Container {
	logx.Info("🔧 Initializing IAM container...")

	c := &Container{}

	// ── Repositories ─────────────────────────────────────────────────────

	tenantRepo := tenantinfra.NewPostgresTenantRepository(deps.DB)
	tenantConfigRepo := tenantinfra.NewPostgresTenantConfigRepository(deps.DB)
	userRepo := userinfra.NewPostgresUserRepository(deps.DB)
	tokenRepo := authinfra.NewPostgresTokenRepository(deps.DB)
	sessionRepo := authinfra.NewPostgresSessionRepository(deps.DB)
	passwordResetRepo := authinfra.NewPostgresPasswordResetRepository(deps.DB)
	invitationRepo := invitationinfra.NewPostgresInvitationRepository(deps.DB)
	apiKeyRepo := apikeyinfra.NewPostgresAPIKeyRepository(deps.DB)
	otpRepo := otpinfra.NewPostgresOTPRepository(deps.DB)

	// ── Infrastructure services ──────────────────────────────────────────

	var stateManager auth.StateManager
	if deps.Cfg.OAuth.StateManager.Type == "redis" {
		stateManager = authinfra.NewRedisStateManager(deps.Redis, deps.Cfg.OAuth.StateManager.TTL)
		logx.Info("  ✅ Using Redis state manager for OAuth")
	} else {
		stateManager = auth.NewInMemoryStateManager(deps.Cfg.OAuth.StateManager.TTL)
		logx.Warn("  ⚠️  Using in-memory state manager (not recommended for production)")
	}

	passwordSvc := authinfra.NewBcryptPasswordService(deps.Cfg.Auth.Password.BcryptCost)

	c.TokenService = auth.NewJWTServiceFromConfig(&deps.Cfg.Auth.JWT)

	apikey.InitAPIKeyConfig(
		deps.Cfg.Auth.APIKey.LivePrefix,
		deps.Cfg.Auth.APIKey.TestPrefix,
		deps.Cfg.Auth.APIKey.TokenLength,
	)

	// ── Domain services ──────────────────────────────────────────────────

	c.TenantService = tenantsrv.NewTenantService(
		tenantRepo,
		tenantConfigRepo,
		userRepo,
		&deps.Cfg.TenantConfig,
	)

	c.UserService = usersrv.NewUserService(
		userRepo,
		tenantRepo,
		passwordSvc,
	)

	c.InvitationService = invitationsrv.NewInvitationService(
		invitationRepo,
		userRepo,
		tenantRepo,
		&deps.Cfg.Auth.Invitation,
	)

	c.APIKeyService = apikeysrv.NewAPIKeyService(
		apiKeyRepo,
		tenantRepo,
		userRepo,
	)

	c.OTPService = otpsrv.NewOTPService(
		otpRepo,
		deps.OTPNotifier,
		&deps.Cfg.Auth.OTP,
	)

	// ── OAuth providers ──────────────────────────────────────────────────

	oauthServices := make(map[iam.OAuthProvider]auth.OAuthService)

	if deps.Cfg.OAuth.Google.Enabled {
		oauthServices[iam.OAuthProviderGoogle] = auth.NewGoogleOAuthServiceFromConfig(
			&deps.Cfg.OAuth.Google,
			stateManager,
		)
		logx.Info("  ✅ Google OAuth enabled")
	}

	if deps.Cfg.OAuth.Microsoft.Enabled {
		oauthServices[iam.OAuthProviderMicrosoft] = auth.NewMicrosoftOAuthServiceFromConfig(
			&deps.Cfg.OAuth.Microsoft,
			stateManager,
		)
		logx.Info("  ✅ Microsoft OAuth enabled")
	}

	// ── Audit service ────────────────────────────────────────────────────

	auditService := authinfra.NewLogxAuditService()

	// ── Auth handlers ────────────────────────────────────────────────────

	c.OAuthHandlers = auth.NewAuthHandlers(
		oauthServices,
		c.TokenService,
		userRepo,
		tenantRepo,
		tokenRepo,
		sessionRepo,
		stateManager,
		invitationRepo,
		auditService,
		deps.Cfg,
	)

	c.PasswordlessHandlers = auth.NewPasswordlessAuthHandlers(
		c.TokenService,
		userRepo,
		tenantRepo,
		tokenRepo,
		sessionRepo,
		invitationRepo,
		c.OTPService,
		auditService,
		deps.Cfg,
	)

	// ── API handlers ─────────────────────────────────────────────────────

	c.APIKeyHandlers = apikeyapi.NewAPIKeyHandlers(c.APIKeyService)
	c.InvitationHandlers = invitationapi.NewInvitationHandlers(c.InvitationService)

	// ── Middleware ────────────────────────────────────────────────────────

	c.AuthMiddleware = auth.NewAuthMiddleware(c.TokenService)
	c.UnifiedAuthMiddleware = auth.NewAPIKeyMiddleware(c.APIKeyService, c.TokenService)

	// ── Background services ──────────────────────────────────────────────

	c.CleanupService = authinfra.NewCleanupService(
		tokenRepo,
		sessionRepo,
		passwordResetRepo,
		deps.Cfg.Auth.Session.CleanupInterval,
	)

	logx.Info("✅ IAM container initialized")
	return c
}

// StartBackgroundServices starts IAM-specific background workers.
func (c *Container) StartBackgroundServices(ctx context.Context) {
	go c.CleanupService.Start(ctx)
	logx.Info("  ✅ IAM cleanup service started")
}

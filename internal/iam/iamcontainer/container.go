package iamcontainer

import (
	"context"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey/apikeyinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey/apikeysrv"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/auth/authinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation/invitationinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation/invitationsrv"
	"github.com/Abraxas-365/manifesto/internal/iam/otp"
	"github.com/Abraxas-365/manifesto/internal/iam/otp/otpinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/otp/otpsrv"
	"github.com/Abraxas-365/manifesto/internal/iam/role/roleinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/role/rolesrv"
	"github.com/Abraxas-365/manifesto/internal/iam/scopes/scopeapi"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantapi"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantsrv"
	"github.com/Abraxas-365/manifesto/internal/iam/user/userinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/user/usersrv"
	"github.com/Abraxas-365/manifesto/internal/logx"
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

	// InvitationNotifier sends invitation emails when new invitations are created.
	// If nil, no emails are sent (invitations are still created).
	InvitationNotifier invitation.NotificationService
}

// ---------------------------------------------------------------------------
// Container: the public surface of the IAM module.
// Services are exposed for developers to wire their own handlers.
// Only auth and tenant handlers are pre-wired (auth is essential,
// tenant routes were designed explicitly for platform admin vs self-service).
// ---------------------------------------------------------------------------

type Container struct {
	// Services — available for cross-module consumption and custom handlers
	UserService       *usersrv.UserService
	TenantService     *tenantsrv.TenantService
	InvitationService *invitationsrv.InvitationService
	APIKeyService     *apikeysrv.APIKeyService
	OTPService        *otpsrv.OTPService
	RoleService       *rolesrv.RoleService
	TokenService      auth.TokenService

	// Auth handlers — needed by cmd/ to register routes
	OAuthHandlers        *auth.AuthHandlers
	PasswordlessHandlers *auth.PasswordlessAuthHandlers

	// Scope catalog handler — read-only endpoint listing available scopes
	ScopeCatalogHandler *scopeapi.ScopeCatalogHandler

	// Tenant handlers — platform admin + self-service
	TenantHandlers         *tenantapi.TenantHandlers
	PlatformTenantHandlers *tenantapi.PlatformTenantHandlers

	// Middleware — needed by cmd/ to protect route groups
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

	roleRepo := roleinfra.NewPostgresRoleRepository(deps.DB)

	c.InvitationService = invitationsrv.NewInvitationService(
		invitationRepo,
		userRepo,
		tenantRepo,
		roleRepo,
		deps.InvitationNotifier,
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

	c.RoleService = rolesrv.NewRoleService(
		roleRepo,
		tenantRepo,
		userRepo,
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
		roleRepo,
		auditService,
		c.RoleService,
		deps.Cfg,
	)

	c.PasswordlessHandlers = auth.NewPasswordlessAuthHandlers(
		c.TokenService,
		userRepo,
		tenantRepo,
		tokenRepo,
		sessionRepo,
		invitationRepo,
		roleRepo,
		c.OTPService,
		auditService,
		c.RoleService,
		deps.Cfg,
	)

	// ── API handlers ─────────────────────────────────────────────────────

	c.ScopeCatalogHandler = scopeapi.NewScopeCatalogHandler()
	c.TenantHandlers = tenantapi.NewTenantHandlers(c.TenantService)
	c.PlatformTenantHandlers = tenantapi.NewPlatformTenantHandlers(c.TenantService)

	// ── Middleware ────────────────────────────────────────────────────────

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

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/fsx"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxlocal"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxs3"
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
	"github.com/Abraxas-365/manifesto/pkg/iam/otp/otpinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp/otpsrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant/tenantinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant/tenantsrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/user/userinfra"
	"github.com/Abraxas-365/manifesto/pkg/iam/user/usersrv"
	"github.com/Abraxas-365/manifesto/pkg/logx"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Container holds all application dependencies
type Container struct {
	// Config
	Config *config.Config

	// Infrastructure
	DB         *sqlx.DB
	Redis      *redis.Client
	FileSystem fsx.FileSystem
	S3Client   *s3.Client

	// Core IAM Services
	OAuthHandlers        *auth.AuthHandlers             // OAuth authentication
	PasswordlessHandlers *auth.PasswordlessAuthHandlers // Passwordless (OTP) authentication
	TokenService         auth.TokenService
	APIKeyService        *apikeysrv.APIKeyService
	TenantService        *tenantsrv.TenantService
	UserService          *usersrv.UserService
	InvitationService    *invitationsrv.InvitationService
	OTPService           *otpsrv.OTPService

	// API Handlers
	APIKeyHandlers     *apikeyapi.APIKeyHandlers
	InvitationHandlers *invitationapi.InvitationHandlers

	// Middleware
	UnifiedAuthMiddleware *auth.UnifiedAuthMiddleware
	AuthMiddleware        *auth.TokenMiddleware

	// Background Services
	CleanupService *authinfra.CleanupService
}

// NewContainer initializes the dependency injection container
func NewContainer(cfg *config.Config) *Container {
	logx.Info("🔧 Initializing dependency container...")

	c := &Container{
		Config: cfg,
	}

	c.initInfrastructure()
	c.initRepositories()

	logx.Info("✅ Container initialized successfully")
	return c
}

func (c *Container) initInfrastructure() {
	logx.Info("🏗️ Initializing infrastructure...")

	// 1. Database Connection
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Config.Database.Host,
		c.Config.Database.Port,
		c.Config.Database.User,
		c.Config.Database.Password,
		c.Config.Database.Name,
		c.Config.Database.SSLMode,
	)

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		logx.Fatalf("Failed to connect to database: %v", err)
	}
	db.SetMaxOpenConns(c.Config.Database.MaxOpenConns)
	db.SetMaxIdleConns(c.Config.Database.MaxIdleConns)
	db.SetConnMaxLifetime(c.Config.Database.ConnMaxLifetime)
	c.DB = db
	logx.Info("✅ Database connected")

	// 2. Redis Connection
	c.Redis = redis.NewClient(&redis.Options{
		Addr:     c.Config.Redis.Address(),
		Password: c.Config.Redis.Password,
		DB:       c.Config.Redis.DB,
	})
	if _, err := c.Redis.Ping(context.Background()).Result(); err != nil {
		logx.Fatalf("Failed to connect to Redis: %v (Redis is required)", err)
	} else {
		logx.Info("✅ Redis connected")
	}

	// 3. File Storage Configuration (Local or S3)
	c.initFileStorage()

	logx.Info("✅ Infrastructure initialized")
}

func (c *Container) initFileStorage() {
	storageMode := getEnv("STORAGE_MODE", "local") // "local" or "s3"

	switch storageMode {
	case "s3":
		// AWS S3 Configuration
		awsRegion := getEnv("AWS_REGION", c.Config.Email.AWSRegion)
		awsBucket := getEnv("AWS_BUCKET", "manifesto-uploads")

		cfg, err := awsConfig.LoadDefaultConfig(context.TODO(), awsConfig.WithRegion(awsRegion))
		if err != nil {
			logx.Fatalf("Unable to load AWS SDK config: %v", err)
		}
		c.S3Client = s3.NewFromConfig(cfg)
		c.FileSystem = fsxs3.NewS3FileSystem(c.S3Client, awsBucket, "")
		logx.Infof("✅ S3 file system configured (bucket: %s, region: %s)", awsBucket, awsRegion)

	case "local":
		// Local File System
		uploadDir := getEnv("UPLOAD_DIR", "./uploads")
		localFS, err := fsxlocal.NewLocalFileSystem(uploadDir)
		if err != nil {
			logx.Fatalf("Failed to initialize local file system: %v", err)
		}
		c.FileSystem = localFS
		logx.Infof("✅ Local file system configured (path: %s)", localFS.GetBasePath())

	default:
		logx.Fatalf("Unknown STORAGE_MODE: %s (use 'local' or 's3')", storageMode)
	}
}

func (c *Container) initRepositories() {
	logx.Info("🗄️  Initializing repositories and services...")

	// --- IAM Repositories ---
	tenantRepo := tenantinfra.NewPostgresTenantRepository(c.DB)
	tenantConfigRepo := tenantinfra.NewPostgresTenantConfigRepository(c.DB)
	userRepo := userinfra.NewPostgresUserRepository(c.DB)
	tokenRepo := authinfra.NewPostgresTokenRepository(c.DB)
	sessionRepo := authinfra.NewPostgresSessionRepository(c.DB)
	passwordResetRepo := authinfra.NewPostgresPasswordResetRepository(c.DB)
	invitationRepo := invitationinfra.NewPostgresInvitationRepository(c.DB)
	apiKeyRepo := apikeyinfra.NewPostgresAPIKeyRepository(c.DB)
	otpRepo := otpinfra.NewPostgresOTPRepository(c.DB)

	// --- Infrastructure Services ---

	// State Manager (use Redis in production, memory in dev)
	var stateManager auth.StateManager
	if c.Config.OAuth.StateManager.Type == "redis" {
		stateManager = authinfra.NewRedisStateManager(c.Redis, c.Config.OAuth.StateManager.TTL)
		logx.Info("✅ Using Redis state manager for OAuth")
	} else {
		stateManager = auth.NewInMemoryStateManager(c.Config.OAuth.StateManager.TTL)
		logx.Warn("⚠️  Using in-memory state manager (not recommended for production)")
	}

	// Password Service
	passwordSvc := authinfra.NewBcryptPasswordService(c.Config.Auth.Password.BcryptCost)

	// Token Service
	c.TokenService = auth.NewJWTServiceFromConfig(&c.Config.Auth.JWT)

	// Initialize API Key configuration
	apikey.InitAPIKeyConfig(
		c.Config.Auth.APIKey.LivePrefix,
		c.Config.Auth.APIKey.TestPrefix,
		c.Config.Auth.APIKey.TokenLength,
	)

	// --- IAM Domain Services ---
	c.TenantService = tenantsrv.NewTenantService(
		tenantRepo,
		tenantConfigRepo,
		userRepo,
		&c.Config.TenantConfig,
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
		&c.Config.Auth.Invitation,
	)

	c.APIKeyService = apikeysrv.NewAPIKeyService(
		apiKeyRepo,
		tenantRepo,
		userRepo,
	)

	// OTP Service with Console Notifier (development)
	// TODO: Replace with real email service in production
	c.OTPService = otpsrv.NewOTPService(
		otpRepo,
		NewConsoleNotifier(),
		&c.Config.Auth.OTP,
	)

	// --- OAuth Services Map ---
	oauthServices := make(map[iam.OAuthProvider]auth.OAuthService)

	// Google OAuth (if enabled)
	if c.Config.OAuth.Google.Enabled {
		oauthServices[iam.OAuthProviderGoogle] = auth.NewGoogleOAuthServiceFromConfig(
			&c.Config.OAuth.Google,
			stateManager,
		)
		logx.Info("✅ Google OAuth enabled")
	}

	// Microsoft OAuth (if enabled)
	if c.Config.OAuth.Microsoft.Enabled {
		oauthServices[iam.OAuthProviderMicrosoft] = auth.NewMicrosoftOAuthServiceFromConfig(
			&c.Config.OAuth.Microsoft,
			stateManager,
		)
		logx.Info("✅ Microsoft OAuth enabled")
	}

	// --- Authentication Handlers ---

	// OAuth Handler
	c.OAuthHandlers = auth.NewAuthHandlers(
		oauthServices,
		c.TokenService,
		userRepo,
		tenantRepo,
		tokenRepo,
		sessionRepo,
		stateManager,
		invitationRepo,
		c.Config,
	)
	logx.Info("✅ OAuth handlers initialized")

	// Passwordless Handler
	c.PasswordlessHandlers = auth.NewPasswordlessAuthHandlers(
		c.TokenService,
		userRepo,
		tenantRepo,
		tokenRepo,
		sessionRepo,
		invitationRepo,
		c.OTPService,
		c.Config,
	)
	logx.Info("✅ Passwordless handlers initialized")

	// --- API Handlers ---
	c.APIKeyHandlers = apikeyapi.NewAPIKeyHandlers(c.APIKeyService)
	c.InvitationHandlers = invitationapi.NewInvitationHandlers(c.InvitationService)

	// --- Middleware ---
	c.AuthMiddleware = auth.NewAuthMiddleware(c.TokenService)
	c.UnifiedAuthMiddleware = auth.NewAPIKeyMiddleware(c.APIKeyService, c.TokenService)

	// --- Background Services ---
	c.CleanupService = authinfra.NewCleanupService(
		tokenRepo,
		sessionRepo,
		passwordResetRepo,
		c.Config.Auth.Session.CleanupInterval,
	)

	logx.Info("✅ All services and handlers initialized")
}

// StartBackgroundServices starts background workers
func (c *Container) StartBackgroundServices(ctx context.Context) {
	logx.Info("🔄 Starting background services...")

	// Start cleanup service
	go c.CleanupService.Start(ctx)
	logx.Info("✅ Cleanup service started")
}

// Cleanup closes all connections and stops workers
func (c *Container) Cleanup() {
	logx.Info("🧹 Cleaning up resources...")

	// Close database connection
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			logx.Errorf("Error closing database: %v", err)
		} else {
			logx.Info("✅ Database connection closed")
		}
	}

	// Close Redis connection
	if c.Redis != nil {
		if err := c.Redis.Close(); err != nil {
			logx.Errorf("Error closing Redis: %v", err)
		} else {
			logx.Info("✅ Redis connection closed")
		}
	}

	logx.Info("✅ Cleanup completed")
}

// ============================================================================
// Helper Functions
// ============================================================================

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// ============================================================================
// Console Notifier for OTP (Development)
// ============================================================================

// ConsoleNotifier implements the NotificationService interface
// by printing OTP codes to the terminal/console
type ConsoleNotifier struct{}

// NewConsoleNotifier creates a new console-based OTP notifier
func NewConsoleNotifier() *ConsoleNotifier {
	return &ConsoleNotifier{}
}

// SendOTP prints the OTP code to the terminal
func (n *ConsoleNotifier) SendOTP(ctx context.Context, contact string, code string) error {
	fmt.Println("\n" + repeatString("=", 60))
	fmt.Println("📧 OTP NOTIFICATION (Console Output)")
	fmt.Println(repeatString("=", 60))
	fmt.Printf("📨 To: %s\n", contact)
	fmt.Printf("🔐 Code: %s\n", code)
	fmt.Println(repeatString("=", 60))
	fmt.Println("⚠️  This is console output for development only")
	fmt.Println("⚠️  In production, configure email service in config")
	fmt.Println(repeatString("=", 60) + "\n")

	logx.Infof("📧 OTP sent to %s: %s", contact, code)
	return nil
}

func repeatString(s string, count int) string {
	result := ""
	for range count {
		result += s
	}
	return result
}


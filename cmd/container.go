package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/fsx"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxlocal"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxs3"
	"github.com/Abraxas-365/manifesto/pkg/iam"
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
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Container holds all application dependencies
type Container struct {
	// Config
	AuthConfig auth.Config

	// Infrastructure
	DB         *sqlx.DB
	Redis      *redis.Client
	FileSystem fsx.FileSystem
	S3Client   *s3.Client

	// Core IAM Services
	AuthService       *auth.AuthHandlers
	TokenService      auth.TokenService
	APIKeyService     *apikeysrv.APIKeyService
	TenantService     *tenantsrv.TenantService
	UserService       *usersrv.UserService
	InvitationService *invitationsrv.InvitationService
	OTPService        *otpsrv.OTPService

	// API Handlers
	APIKeyHandlers     *apikeyapi.APIKeyHandlers
	InvitationHandlers *invitationapi.InvitationHandlers

	// Middleware
	UnifiedAuthMiddleware *auth.UnifiedAuthMiddleware
	AuthMiddleware        *auth.TokenMiddleware
}

// NewContainer initializes the dependency injection container
func NewContainer() *Container {
	logx.Info("🔧 Initializing dependency container...")

	c := &Container{}
	c.initInfrastructure()
	c.initRepositories()

	logx.Info("✅ Container initialized successfully")
	return c
}

func (c *Container) initInfrastructure() {
	logx.Info("🏗️ Initializing infrastructure...")

	// 1. Database Connection
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPass := getEnv("DB_PASS", "postgres")
	dbName := getEnv("DB_NAME", "manifesto")
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		logx.Fatalf("Failed to connect to database: %v", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	c.DB = db
	logx.Info("✅ Database connected")

	// 2. Redis Connection
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPass := getEnv("REDIS_PASS", "")
	redisDB := getEnvInt("REDIS_DB", 0)
	c.Redis = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPass,
		DB:       redisDB,
	})
	if _, err := c.Redis.Ping(context.Background()).Result(); err != nil {
		logx.Fatalf("Failed to connect to Redis: %v (Redis is required for job queue)", err)
	} else {
		logx.Info("✅ Redis connected")
	}

	// 3. File Storage Configuration (Local or S3)
	storageMode := getEnv("STORAGE_MODE", "local") // "local" or "s3"

	switch storageMode {
	case "s3":
		// AWS S3 Configuration
		awsRegion := getEnv("AWS_REGION", "us-east-1")
		awsBucket := getEnv("AWS_BUCKET", "manifesto-uploads")
		cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(awsRegion))
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

	// 4. Auth Config
	c.AuthConfig = auth.DefaultConfig()
	c.AuthConfig.JWT.SecretKey = getEnv("JWT_SECRET", "")
	if c.AuthConfig.JWT.SecretKey == "" {
		logx.Warn("⚠️  JWT_SECRET is not set, using default (UNSAFE for production)")
		c.AuthConfig.JWT.SecretKey = "super-secret-key-please-change-me-in-production"
	}

	// OAuth Configs
	c.AuthConfig.OAuth.Google.ClientID = getEnv("GOOGLE_CLIENT_ID", "")
	c.AuthConfig.OAuth.Google.ClientSecret = getEnv("GOOGLE_CLIENT_SECRET", "")
	c.AuthConfig.OAuth.Google.RedirectURL = getEnv("GOOGLE_REDIRECT_URL", "")
}

func (c *Container) initRepositories() {
	logx.Info("🗄️  Initializing repositories and services...")

	// --- IAM Repositories ---
	tenantRepo := tenantinfra.NewPostgresTenantRepository(c.DB)
	tenantConfigRepo := tenantinfra.NewPostgresTenantConfigRepository(c.DB)
	userRepo := userinfra.NewPostgresUserRepository(c.DB)
	tokenRepo := authinfra.NewPostgresTokenRepository(c.DB)
	sessionRepo := authinfra.NewPostgresSessionRepository(c.DB)
	invitationRepo := invitationinfra.NewPostgresInvitationRepository(c.DB)
	apiKeyRepo := apikeyinfra.NewPostgresAPIKeyRepository(c.DB)
	otpRepo := otpinfra.NewPostgresOTPRepository(c.DB)

	// --- Infrastructure Services ---
	stateManager := authinfra.NewRedisStateManager(c.Redis)
	passwordSvc := authinfra.NewBcryptPasswordService()

	// Token Service
	c.TokenService = auth.NewJWTService(
		c.AuthConfig.JWT.SecretKey,
		c.AuthConfig.JWT.AccessTokenTTL,
		c.AuthConfig.JWT.RefreshTokenTTL,
		c.AuthConfig.JWT.Issuer,
	)

	// --- IAM Domain Services ---
	c.TenantService = tenantsrv.NewTenantService(tenantRepo, tenantConfigRepo, userRepo)
	c.UserService = usersrv.NewUserService(userRepo, tenantRepo, passwordSvc)
	c.InvitationService = invitationsrv.NewInvitationService(invitationRepo, userRepo, tenantRepo)
	c.APIKeyService = apikeysrv.NewAPIKeyService(apiKeyRepo, tenantRepo, userRepo)
	c.OTPService = otpsrv.NewOTPService(otpRepo, NewConsoleNotifier())

	// OAuth Services Map
	oauthServices := map[iam.OAuthProvider]auth.OAuthService{
		iam.OAuthProviderGoogle: auth.NewGoogleOAuthService(c.AuthConfig.OAuth.Google, stateManager),
		// Add Microsoft if configured
	}

	// Auth Handler (Core Logic)
	c.AuthService = auth.NewAuthHandlers(
		oauthServices,
		c.TokenService,
		userRepo,
		tenantRepo,
		tokenRepo,
		sessionRepo,
		stateManager,
		invitationRepo,
	)

	// --- API Handlers ---
	c.APIKeyHandlers = apikeyapi.NewAPIKeyHandlers(c.APIKeyService)
	c.InvitationHandlers = invitationapi.NewInvitationHandlers(c.InvitationService)

	// --- Middleware ---
	c.AuthMiddleware = auth.NewAuthMiddleware(c.TokenService)
	c.UnifiedAuthMiddleware = auth.NewAPIKeyMiddleware(c.APIKeyService, c.TokenService)

	logx.Info("✅ All services and handlers initialized")
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

// getEnvInt gets an environment variable as int with a default value
func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	var intValue int
	if _, err := fmt.Sscanf(value, "%d", &intValue); err != nil {
		logx.Warnf("Invalid integer value for %s: %s, using default: %d", key, value, defaultValue)
		return defaultValue
	}
	return intValue
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
	fmt.Println("=" + repeatString("=", 50))
	fmt.Printf("📧 OTP NOTIFICATION\n")
	fmt.Printf("Contact: %s\n", contact)
	fmt.Printf("Code: %s\n", code)
	fmt.Println("=" + repeatString("=", 50))

	logx.Info(fmt.Sprintf("OTP sent to %s: %s", contact, code))
	return nil
}

func repeatString(s string, count int) string {
	result := ""
	for range count {
		result += s
	}
	return result
}

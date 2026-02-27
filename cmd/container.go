// cmd/container.go
//
// Root composition root. Owns infrastructure (DB, Redis, FS) and composes
// bounded-context containers. This is the only place that knows about ALL modules.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/fsx"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxlocal"
	"github.com/Abraxas-365/manifesto/pkg/fsx/fsxs3"
	"github.com/Abraxas-365/manifesto/pkg/logx"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// Container holds shared infrastructure and composed module containers.
type Container struct {
	Config *config.Config

	// Infrastructure (shared across all modules)
	DB         *sqlx.DB
	Redis      *redis.Client
	FileSystem fsx.FileSystem
	S3Client   *s3.Client

	// Bounded-context containers
	// Add your module containers here
}

func NewContainer(cfg *config.Config) *Container {
	logx.Info("🔧 Initializing application container...")

	c := &Container{Config: cfg}

	c.initInfrastructure()
	c.initModules()

	logx.Info("✅ Application container initialized")
	return c
}

// ---------------------------------------------------------------------------
// Infrastructure — DB, Redis, file storage
// ---------------------------------------------------------------------------

func (c *Container) initInfrastructure() {
	logx.Info("🏗️ Initializing infrastructure...")

	// 1. Database
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
	logx.Info("  ✅ Database connected")

	// 2. Redis
	c.Redis = redis.NewClient(&redis.Options{
		Addr:     c.Config.Redis.Address(),
		Password: c.Config.Redis.Password,
		DB:       c.Config.Redis.DB,
	})
	if _, err := c.Redis.Ping(context.Background()).Result(); err != nil {
		logx.Fatalf("Failed to connect to Redis: %v (Redis is required)", err)
	}
	logx.Info("  ✅ Redis connected")

	// 3. File storage
	c.initFileStorage()

	logx.Info("✅ Infrastructure initialized")
}

func (c *Container) initFileStorage() {
	storageMode := getEnv("STORAGE_MODE", "local")

	switch storageMode {
	case "s3":
		awsRegion := getEnv("AWS_REGION", "us-east-1")
		awsBucket := getEnv("AWS_BUCKET", "manifesto-uploads")

		cfg, err := awsConfig.LoadDefaultConfig(context.TODO(), awsConfig.WithRegion(awsRegion))
		if err != nil {
			logx.Fatalf("Unable to load AWS SDK config: %v", err)
		}
		c.S3Client = s3.NewFromConfig(cfg)
		c.FileSystem = fsxs3.NewS3FileSystem(c.S3Client, awsBucket, "")
		logx.Infof("  ✅ S3 file system configured (bucket: %s, region: %s)", awsBucket, awsRegion)

	case "local":
		uploadDir := getEnv("UPLOAD_DIR", "./uploads")
		localFS, err := fsxlocal.NewLocalFileSystem(uploadDir)
		if err != nil {
			logx.Fatalf("Failed to initialize local file system: %v", err)
		}
		c.FileSystem = localFS
		logx.Infof("  ✅ Local file system configured (path: %s)", localFS.GetBasePath())

	default:
		logx.Fatalf("Unknown STORAGE_MODE: %s (use 'local' or 's3')", storageMode)
	}
}

// ---------------------------------------------------------------------------
// Module composition — each bounded context wires itself
// ---------------------------------------------------------------------------

func (c *Container) initModules() {
	logx.Info("📦 Initializing modules...")
	// Add your module initialization here
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func (c *Container) StartBackgroundServices(ctx context.Context) {
	logx.Info("🔄 Starting background services...")
	// Add your background services here
}

func (c *Container) Cleanup() {
	logx.Info("🧹 Cleaning up resources...")

	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			logx.Errorf("Error closing database: %v", err)
		} else {
			logx.Info("  ✅ Database connection closed")
		}
	}

	if c.Redis != nil {
		if err := c.Redis.Close(); err != nil {
			logx.Errorf("Error closing Redis: %v", err)
		} else {
			logx.Info("  ✅ Redis connection closed")
		}
	}

	logx.Info("✅ Cleanup complete")
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func repeatString(s string, count int) string {
	result := ""
	for range count {
		result += s
	}
	return result
}

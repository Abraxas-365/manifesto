// server.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/logx"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		logx.Fatalf("Failed to load configuration: %v", err)
	}

	// 2. Initialize Logger with config
	switch cfg.Server.LogLevel {
	case "debug":
		logx.SetLevel(logx.LevelDebug)
	case "warn":
		logx.SetLevel(logx.LevelWarn)
	case "error":
		logx.SetLevel(logx.LevelError)
	default:
		logx.SetLevel(logx.LevelInfo)
	}

	logx.Info("🚀 Starting Manifesto API Server...")
	logx.Infof("Environment: %s", cfg.Server.Environment)

	// 3. Initialize Dependency Container
	container := NewContainer(cfg)
	defer container.Cleanup()

	// 4. Start background services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	container.StartBackgroundServices(ctx)

	// 5. Create Fiber App with Config
	app := fiber.New(fiber.Config{
		AppName:               "Manifesto API",
		DisableStartupMessage: true,
		ErrorHandler:          globalErrorHandler(cfg),
		BodyLimit:             10 * 1024 * 1024, // 10MB for file uploads
		IdleTimeout:           120,
		EnablePrintRoutes:     false,
	})

	// 6. Global Middleware
	setupMiddleware(app, cfg)

	// 7. Health Check & Info Endpoints
	app.Get("/health", healthCheckHandler(container))
	app.Get("/", infoHandler(cfg))
	app.Get("/api/v1/docs", apiDocsHandler(cfg))

	// 8. Register Routes
	registerRoutes(app, container)

	// 9. 404 Handler
	app.Use(notFoundHandler)

	// 10. Print Route Summary
	printRouteSummary()

	// 11. Start Server with Graceful Shutdown
	startServer(app, cfg, cancel)
}

// ============================================================================
// Setup Functions
// ============================================================================

func setupMiddleware(app *fiber.App, cfg *config.Config) {
	// Panic recovery
	app.Use(recover.New(recover.Config{
		EnableStackTrace: cfg.IsDevelopment(),
	}))

	// Request ID
	app.Use(requestid.New(requestid.Config{
		Header: "X-Request-ID",
		Generator: func() string {
			return generateRequestID()
		},
	}))

	// CORS
	corsOrigins := "*"
	if len(cfg.Server.CORSOrigins) > 0 {
		corsOrigins = ""
		for i, origin := range cfg.Server.CORSOrigins {
			if i > 0 {
				corsOrigins += ","
			}
			corsOrigins += origin
		}
	}

	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-API-Key, X-Request-ID",
		AllowMethods:     "GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS",
		AllowCredentials: true,
		ExposeHeaders:    "X-Request-ID",
	}))

	// Request logger
	logFormat := "${time} | ${status} | ${latency} | ${method} ${path}"
	if cfg.IsDevelopment() {
		logFormat += " | ${ip} | ${reqHeader:X-Request-ID}\n"
	} else {
		logFormat += "\n"
	}

	app.Use(logger.New(logger.Config{
		Format:     logFormat,
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "Local",
	}))
}

func registerRoutes(app *fiber.App, container *Container) {
	logx.Info("📝 Registering routes...")

	// Add your module routes here
	// Example:
	// api := app.Group("/api/v1")
	// myModule.RegisterRoutes(api)

	logx.Info("✅ All routes registered")
}

// ============================================================================
// Handler Functions
// ============================================================================

// healthCheckHandler returns a health check handler
func healthCheckHandler(container *Container) fiber.Handler {
	return func(c *fiber.Ctx) error {
		health := fiber.Map{
			"status":      "healthy",
			"service":     "manifesto-api",
			"version":     container.Config.Server.BaseURL,
			"environment": container.Config.Server.Environment,
			"timestamp":   fmt.Sprintf("%d", c.Context().Time().Unix()),
		}

		// Check database
		if err := container.DB.Ping(); err != nil {
			health["db"] = "unhealthy"
			health["db_error"] = err.Error()
			health["status"] = "degraded"
		} else {
			health["db"] = "healthy"
		}

		// Check Redis
		if _, err := container.Redis.Ping(c.Context()).Result(); err != nil {
			health["redis"] = "unhealthy"
			health["redis_error"] = err.Error()
			health["status"] = "degraded"
		} else {
			health["redis"] = "healthy"
		}

		// Check storage (optional - can be slow)
		checkStorage := c.QueryBool("check_storage", false)
		if checkStorage {
			if exists, err := container.FileSystem.Exists(c.Context(), ".health-check"); err != nil {
				health["storage"] = "unhealthy"
				health["storage_error"] = err.Error()
			} else {
				health["storage"] = "healthy"
				health["storage_accessible"] = exists
			}
		}

		status := fiber.StatusOK
		if health["status"] == "degraded" {
			status = fiber.StatusServiceUnavailable
		}

		return c.Status(status).JSON(health)
	}
}

// infoHandler returns basic API information
func infoHandler(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service":     "Manifesto API",
			"version":     "1.0.0",
			"environment": cfg.Server.Environment,
			"endpoints": fiber.Map{
				"docs":   "/api/v1/docs",
				"health": "/health",
			},
		})
	}
}

// apiDocsHandler returns API documentation
func apiDocsHandler(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"api_version": "v1",
			"base_url":    cfg.Server.BaseURL,
			"endpoints":   fiber.Map{},
		})
	}
}

// notFoundHandler handles 404 errors
func notFoundHandler(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error":      "Route not found",
		"code":       "NOT_FOUND",
		"path":       c.Path(),
		"method":     c.Method(),
		"message":    "The requested endpoint does not exist. Visit /api/v1/docs for documentation.",
		"request_id": c.Get("X-Request-ID"),
	})
}

// ============================================================================
// Error Handler
// ============================================================================

// globalErrorHandler converts internal errors to standard HTTP responses
func globalErrorHandler(cfg *config.Config) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		// Log the error with context
		logx.WithFields(logx.Fields{
			"path":       c.Path(),
			"method":     c.Method(),
			"ip":         c.IP(),
			"request_id": c.Get("X-Request-ID"),
			"user_agent": c.Get("User-Agent"),
		}).Errorf("Request error: %v", err)

		// If it's a Fiber error
		if e, ok := err.(*fiber.Error); ok {
			return c.Status(e.Code).JSON(fiber.Map{
				"error":      e.Message,
				"code":       "FIBER_ERROR",
				"status":     e.Code,
				"request_id": c.Get("X-Request-ID"),
			})
		}

		// If it's our custom errx.Error
		if e, ok := err.(*errx.Error); ok {
			response := fiber.Map{
				"error":      e.Message,
				"code":       e.Code,
				"type":       string(e.Type),
				"status":     e.HTTPStatus,
				"request_id": c.Get("X-Request-ID"),
			}

			// Include details if present
			if len(e.Details) > 0 {
				response["details"] = e.Details
			}

			// Include underlying error in debug mode
			if cfg.IsDevelopment() && e.Err != nil {
				response["underlying_error"] = e.Err.Error()
			}

			return c.Status(e.HTTPStatus).JSON(response)
		}

		// Default unknown error
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":      "Internal Server Error",
			"type":       "INTERNAL",
			"code":       "INTERNAL_ERROR",
			"message":    "An unexpected error occurred. Please contact support if the issue persists.",
			"request_id": c.Get("X-Request-ID"),
		})
	}
}

// ============================================================================
// Utility Functions
// ============================================================================

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return "req-" + randomString(16)
}

// randomString generates a random string of given length
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

// printRouteSummary prints a summary of registered routes
func printRouteSummary() {
	logx.Info("📋 Route Summary:")
	logx.Info("   ├─ Health: /health")
	logx.Info("   ├─ Info: /")
	logx.Info("   └─ Docs: /api/v1/docs")
}

// startServer starts the server with graceful shutdown
func startServer(app *fiber.App, cfg *config.Config, cancel context.CancelFunc) {
	port := fmt.Sprintf("%d", cfg.Server.Port)

	// Run server in a goroutine
	go func() {
		logx.Info("=" + repeatString("=", 70))
		logx.Infof("🚀 Server listening on port %s", port)
		logx.Infof("📚 API Docs: http://localhost:%s/api/v1/docs", port)
		logx.Infof("💚 Health Check: http://localhost:%s/health", port)
		logx.Infof("🔒 Environment: %s", cfg.Server.Environment)

		logx.Info("=" + repeatString("=", 70))

		if err := app.Listen(":" + port); err != nil {
			logx.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	gracefulShutdown(app, cancel)
}

// gracefulShutdown handles graceful server shutdown
func gracefulShutdown(app *fiber.App, cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Wait for interrupt signal
	sig := <-sigChan
	logx.Infof("🛑 Received signal: %v", sig)
	logx.Info("Shutting down gracefully...")

	// Cancel context to stop background services
	cancel()

	// Shutdown the server with timeout
	if err := app.ShutdownWithTimeout(30); err != nil {
		logx.Errorf("Server forced to shutdown: %v", err)
	}

	logx.Info("✅ Server exited successfully")
}

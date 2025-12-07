package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/logx"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	// 1. Initialize Logger
	logLevel := getEnv("LOG_LEVEL", "info")
	switch logLevel {
	case "debug":
		logx.SetLevel(logx.LevelDebug)
	case "warn":
		logx.SetLevel(logx.LevelWarn)
	case "error":
		logx.SetLevel(logx.LevelError)
	default:
		logx.SetLevel(logx.LevelInfo)
	}

	logx.Info("🚀 Starting Relay ATS API Server...")

	// 2. Initialize Dependency Container
	container := NewContainer()
	defer container.Cleanup()

	// 3. Create Fiber App with Config
	app := fiber.New(fiber.Config{
		AppName:               "Relay ATS API",
		DisableStartupMessage: true,
		ErrorHandler:          globalErrorHandler,
		BodyLimit:             10 * 1024 * 1024, // 10MB for file uploads
		IdleTimeout:           120,
		EnablePrintRoutes:     false,
	})

	// 4. Global Middleware
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	app.Use(requestid.New(requestid.Config{
		Header: "X-Request-ID",
		Generator: func() string {
			return generateRequestID()
		},
	}))

	app.Use(cors.New(cors.Config{
		AllowOrigins: getCORSOrigins(),
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-API-Key, X-Request-ID",
		AllowMethods: "GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS",
		// AllowCredentials: true,
		ExposeHeaders: "X-Request-ID",
	}))

	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${method} ${path} | ${ip} | ${reqHeader:X-Request-ID}\n",
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "Local",
	}))

	// 5. Health Check & Info Endpoints
	app.Get("/health", healthCheckHandler(container))
	app.Get("/", infoHandler)
	app.Get("/api/v1/docs", apiDocsHandler)

	// 6. Register Routes

	// ========================================================================
	// Core Authentication Routes
	// ========================================================================
	// Routes: /auth/login, /auth/refresh, /auth/logout, /auth/me
	container.AuthService.RegisterRoutes(app)
	logx.Info("✓ Auth routes registered")

	// ========================================================================
	// IAM (Identity & Access Management) Routes
	// ========================================================================
	// API Keys Management: /api/v1/api-keys/*
	container.APIKeyHandlers.RegisterRoutes(app, container.UnifiedAuthMiddleware)
	logx.Info("✓ API Key routes registered")

	// Invitations Management: /api/v1/invitations/*
	container.InvitationHandlers.RegisterRoutes(app, container.UnifiedAuthMiddleware)
	logx.Info("✓ Invitation routes registered")

	// 7. 404 Handler
	app.Use(notFoundHandler)

	// 8. Print Route Summary
	printRouteSummary()

	// 9. Start Server with Graceful Shutdown
	startServer(app)
}

// ============================================================================
// Handler Functions
// ============================================================================

// healthCheckHandler returns a health check handler
func healthCheckHandler(container *Container) fiber.Handler {
	return func(c *fiber.Ctx) error {
		health := fiber.Map{
			"status":  "healthy",
			"service": "relay-ats-api",
			"version": getEnv("APP_VERSION", "1.0.0"),
		}

		// Check database
		if err := container.DB.Ping(); err != nil {
			health["db"] = "unhealthy"
			health["db_error"] = err.Error()
			health["status"] = "degraded"
		} else {
			health["db"] = "healthy"
		}

		// Check S3 (optional - can be slow)
		checkS3 := c.QueryBool("check_s3", false)
		if checkS3 {
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
func infoHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"service":     "Relay ATS API",
		"version":     getEnv("APP_VERSION", "1.0.0"),
		"description": "AI-Powered Applicant Tracking System",
		"features": []string{
			"Multi-tenant architecture",
			"OAuth authentication",
			"API key management",
		},
		"endpoints": fiber.Map{
			"docs":   "/api/v1/docs",
			"health": "/health",
		},
	})
}

// apiDocsHandler returns API documentation
func apiDocsHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"api_version": "v1",
		"base_url":    getEnv("API_BASE_URL", "http://localhost:8080"),
		"endpoints": fiber.Map{
			"authentication": fiber.Map{
				"login":   "POST /auth/login",
				"refresh": "POST /auth/refresh",
				"logout":  "POST /auth/logout",
				"me":      "GET /auth/me",
			},
			"iam": fiber.Map{
				"api_keys": fiber.Map{
					"list":   "GET /api/v1/api-keys",
					"create": "POST /api/v1/api-keys",
					"get":    "GET /api/v1/api-keys/:id",
					"revoke": "DELETE /api/v1/api-keys/:id",
				},
				"invitations": fiber.Map{
					"list":   "GET /api/v1/invitations",
					"create": "POST /api/v1/invitations",
					"accept": "POST /api/v1/invitations/:id/accept",
				},
			},
		},
		"authentication": fiber.Map{
			"types": []string{"JWT", "API Key"},
			"headers": fiber.Map{
				"jwt":     "Authorization: Bearer <token>",
				"api_key": "X-API-Key: <key> OR Authorization: Bearer <key>",
			},
		},
	})
}

// notFoundHandler handles 404 errors
func notFoundHandler(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error":      "Route not found",
		"code":       "NOT_FOUND",
		"path":       c.Path(),
		"method":     c.Method(),
		"message":    "The requested endpoint does not exist",
		"request_id": c.Get("X-Request-ID"),
	})
}

// ============================================================================
// Error Handler
// ============================================================================

// globalErrorHandler converts internal errors to standard HTTP responses
func globalErrorHandler(c *fiber.Ctx, err error) error {
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
		if getEnv("DEBUG", "false") == "true" && e.Err != nil {
			response["underlying_error"] = e.Err.Error()
		}

		return c.Status(e.HTTPStatus).JSON(response)
	}

	// Default unknown error
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error":      "Internal Server Error",
		"type":       "INTERNAL",
		"code":       "INTERNAL_ERROR",
		"message":    "An unexpected error occurred",
		"request_id": c.Get("X-Request-ID"),
	})
}

// ============================================================================
// Utility Functions
// ============================================================================

// getPort returns the port to listen on
func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return port
}

// getCORSOrigins returns allowed CORS origins
func getCORSOrigins() string {
	origins := os.Getenv("CORS_ORIGINS")
	if origins == "" {
		return "*" // Default for development
	}
	return origins
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	// Simple implementation - you can use UUID library
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
	logx.Info("   ├─ Auth: /auth/*")
	logx.Info("   ├─ IAM: /api/v1/api-keys/*, /api/v1/invitations/*")
	logx.Info("   ├─ Health: /health")
	logx.Info("   └─ Docs: /api/v1/docs")
}

// startServer starts the server with graceful shutdown
func startServer(app *fiber.App) {
	port := getPort()

	// Run server in a goroutine
	go func() {
		logx.Info("=" + repeatString("=", 60))
		logx.Infof("🚀 Server listening on port %s", port)
		logx.Infof("📚 API Docs: http://localhost:%s/api/v1/docs", port)
		logx.Infof("💚 Health Check: http://localhost:%s/health", port)
		logx.Info("=" + repeatString("=", 60))

		if err := app.Listen(":" + port); err != nil {
			logx.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	gracefulShutdown(app)
}

// gracefulShutdown handles graceful server shutdown
func gracefulShutdown(app *fiber.App) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Wait for interrupt signal
	sig := <-sigChan
	logx.Infof("🛑 Received signal: %v", sig)
	logx.Info("Shutting down gracefully...")

	// Shutdown the server with timeout
	if err := app.ShutdownWithTimeout(30); err != nil {
		logx.Errorf("Server forced to shutdown: %v", err)
	}

	logx.Info("✅ Server exited successfully")
}

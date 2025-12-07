package auth

import (
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/gofiber/fiber/v2"
)

// ProtectedRoutes helper para configurar rutas protegidas
type ProtectedRoutes struct {
	authMiddleware *TokenMiddleware
}

// NewProtectedRoutes crea un nuevo helper para rutas protegidas
func NewProtectedRoutes(authMiddleware *TokenMiddleware) *ProtectedRoutes {
	return &ProtectedRoutes{
		authMiddleware: authMiddleware,
	}
}

// SetupProtectedRoutes configura rutas protegidas
func (pr *ProtectedRoutes) SetupProtectedRoutes(app *fiber.App) {
	// Rutas públicas
	public := app.Group("/api/public")
	public.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "OK"})
	})

	// Rutas protegidas (requieren autenticación)
	protected := app.Group("/api")
	protected.Use(pr.authMiddleware.Authenticate())

	// Rutas específicas de usuario autenticado
	protected.Get("/me", func(c *fiber.Ctx) error {
		authContext, _ := GetAuthContext(c)
		return c.JSON(fiber.Map{
			"user_id":   authContext.UserID,
			"tenant_id": authContext.TenantID,
			"email":     authContext.Email,
			"name":      authContext.Name,
			"scopes":    authContext.Scopes,
			"is_admin":  authContext.IsAdmin(),
		})
	})

	// Rutas de administración (requieren admin)
	admin := protected.Group("/admin")
	admin.Use(pr.authMiddleware.RequireAdmin())

	admin.Get("/users", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Admin users endpoint"})
	})

	admin.Get("/tenants", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Admin tenants endpoint"})
	})

	// Rutas por tenant
	tenant := protected.Group("/tenant/:tenantId")
	tenant.Use(pr.ValidateTenantAccess())

	tenant.Get("/dashboard", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Tenant dashboard"})
	})
}

// ValidateTenantAccess middleware para validar acceso a tenant específico
func (pr *ProtectedRoutes) ValidateTenantAccess() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantIDParam := c.Params("tenantId")
		authContext, ok := GetAuthContext(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication required",
			})
		}

		// Verificar que el usuario tenga acceso al tenant
		if authContext.TenantID.String() != tenantIDParam && !authContext.IsAdmin() {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Access denied to this tenant",
			})
		}

		// Agregar tenant ID al contexto
		c.Locals("tenant_id", kernel.NewTenantID(tenantIDParam))
		return c.Next()
	}
}

func ValidateTenantAccess() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantIDParam := c.Params("tenantId")
		authContext, ok := GetAuthContext(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication required",
			})
		}

		// Verificar que el usuario tenga acceso al tenant
		// Los admins pueden acceder a cualquier tenant
		if authContext.TenantID.String() != tenantIDParam && !authContext.IsAdmin() {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Access denied to this tenant",
			})
		}

		// Agregar tenant ID al contexto para uso en handlers
		c.Locals("tenant_id", kernel.NewTenantID(tenantIDParam))
		return c.Next()
	}
}

// GetTenantID obtiene el tenant ID del contexto
// Helper function para usar en los handlers
func GetTenantID(c *fiber.Ctx) (kernel.TenantID, bool) {
	tenantID, ok := c.Locals("tenant_id").(kernel.TenantID)
	return tenantID, ok
}

func GetUserID(c *fiber.Ctx) (kernel.UserID, bool) {
	userID, ok := c.Locals("user_id").(kernel.UserID)
	return userID, ok
}

package auth

import (
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/iam"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/gofiber/fiber/v2"
)

// TokenMiddleware middleware para autenticación JWT con Fiber
type TokenMiddleware struct {
	tokenService TokenService
}

// NewAuthMiddleware crea un nuevo middleware de autenticación
func NewAuthMiddleware(tokenService TokenService) *TokenMiddleware {
	return &TokenMiddleware{
		tokenService: tokenService,
	}
}

// Authenticate middleware que valida tokens JWT
func (am *TokenMiddleware) Authenticate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extraer token del header Authorization o cookie de acceso
		authHeader := c.Get("Authorization")
		var token string

		if authHeader != "" {
			// Verificar formato "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "" {
				token = parts[1]
			} else {
				// Fallback: intentar con cookie "access_token" si el header es inválido
				token = c.Cookies("access_token")
				if token == "" {
					return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
						"error": iam.ErrInvalidToken().Error(),
					})
				}
			}
		} else {
			// Fallback: intentar con cookie "access_token"
			token = c.Cookies("access_token")
			if token == "" {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": iam.ErrUnauthorized().Error(),
				})
			}
		}

		// Validar token
		claims, err := am.tokenService.ValidateAccessToken(token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Crear contexto de autenticación
		authContext := &kernel.AuthContext{
			UserID:   &claims.UserID,
			TenantID: claims.TenantID,
			Email:    claims.Email,
			Name:     claims.Name,
			Scopes:   claims.Scopes,
			IsAPIKey: false,
		}

		// Agregar al contexto de Fiber
		c.Locals("auth", authContext)

		return c.Next()
	}
}

// RequireAdmin middleware que requiere permisos de administrador
func (am *TokenMiddleware) RequireAdmin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authContext, ok := c.Locals("auth").(*kernel.AuthContext)
		if !ok || authContext == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": iam.ErrUnauthorized().Error(),
			})
		}

		if !authContext.IsAdmin() {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": iam.ErrAccessDenied().Error(),
			})
		}

		return c.Next()
	}
}

// RequireTenant middleware que valida acceso al tenant
func (am *TokenMiddleware) RequireTenant(tenantID kernel.TenantID) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authContext, ok := c.Locals("auth").(*kernel.AuthContext)
		if !ok || authContext == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": iam.ErrUnauthorized().Error(),
			})
		}

		if authContext.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Access denied for this tenant",
			})
		}

		return c.Next()
	}
}

package auth

import (
	"strings"

	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

// ValidateTenantAccess middleware to validate access to a specific tenant.
// Users with "*" scope can access any tenant.
func ValidateTenantAccess() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantIDParam := c.Params("tenantId")
		authContext, ok := GetAuthContext(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication required",
			})
		}

		if authContext.TenantID.String() != tenantIDParam && !authContext.HasScope("*") {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Access denied to this tenant",
			})
		}

		c.Locals("tenant_id", kernel.NewTenantID(tenantIDParam))
		return c.Next()
	}
}

// GetTenantID retrieves the tenant ID from the request context.
func GetTenantID(c *fiber.Ctx) (kernel.TenantID, bool) {
	tenantID, ok := c.Locals("tenant_id").(kernel.TenantID)
	return tenantID, ok
}

// GetUserID retrieves the user ID from the request context.
func GetUserID(c *fiber.Ctx) (kernel.UserID, bool) {
	userID, ok := c.Locals("user_id").(kernel.UserID)
	return userID, ok
}

// EnsureCanGrantScopes prevents privilege escalation: a caller may only grant
// scopes that they themselves hold. Returns a 403 fiber error listing the
// offending scopes otherwise.
func EnsureCanGrantScopes(authCtx *kernel.AuthContext, requested []string) error {
	var denied []string
	for _, s := range requested {
		if !authCtx.HasScope(s) {
			denied = append(denied, s)
		}
	}
	if len(denied) > 0 {
		return fiber.NewError(fiber.StatusForbidden,
			"cannot grant scopes you do not hold: "+strings.Join(denied, ", "))
	}
	return nil
}

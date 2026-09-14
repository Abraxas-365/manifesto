package tenantapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	iamscopes "github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantsrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// Tenant Self-Service Handlers — tenant owners manage their own tenant
// Uses TenantID from the JWT, no :id parameter.
// ---------------------------------------------------------------------------

type TenantHandlers struct {
	service *tenantsrv.TenantService
}

func NewTenantHandlers(service *tenantsrv.TenantService) *TenantHandlers {
	return &TenantHandlers{service: service}
}

func (h *TenantHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	me := router.Group("/tenants/me", authMiddleware.Authenticate())

	me.Get("/", authMiddleware.RequireScope(iamscopes.ScopeTenantsRead), h.GetMyTenant)
	me.Get("/stats", authMiddleware.RequireScope(iamscopes.ScopeTenantsRead), h.GetMyTenantStats)
	me.Get("/usage", authMiddleware.RequireScope(iamscopes.ScopeTenantsRead), h.GetMyTenantUsage)
	me.Get("/config", authMiddleware.RequireScope(iamscopes.ScopeTenantsConfig), h.GetMyTenantConfig)
	me.Put("/config", authMiddleware.RequireScope(iamscopes.ScopeTenantsConfig), h.SetMyTenantConfig)
	me.Delete("/config/:key", authMiddleware.RequireScope(iamscopes.ScopeTenantsConfig), h.DeleteMyTenantConfig)
}

func (h *TenantHandlers) GetMyTenant(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	response, err := h.service.GetTenantByID(c.Context(), authContext, authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(response.ToDTO())
}

func (h *TenantHandlers) GetMyTenantStats(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	stats, err := h.service.GetTenantStats(c.Context(), authContext, authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(stats)
}

func (h *TenantHandlers) GetMyTenantUsage(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	usage, err := h.service.GetTenantUsage(c.Context(), authContext, authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(usage)
}

func (h *TenantHandlers) GetMyTenantConfig(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	config, err := h.service.GetTenantConfig(c.Context(), authContext, authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(config)
}

func (h *TenantHandlers) SetMyTenantConfig(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	req, err := kernel.BindAndValidate[tenant.SetConfigRequest](c)
	if err != nil {
		return err
	}

	if err := h.service.SetTenantConfig(c.Context(), authContext, authContext.TenantID, req.Key, req.Value); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config saved successfully"})
}

func (h *TenantHandlers) DeleteMyTenantConfig(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return fiber.ErrUnauthorized
	}

	key := c.Params("key")
	if err := h.service.DeleteTenantConfig(c.Context(), authContext, authContext.TenantID, key); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config deleted successfully"})
}

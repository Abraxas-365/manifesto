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
// Platform Admin Handlers — cross-tenant operations for platform operators
// ---------------------------------------------------------------------------

type PlatformTenantHandlers struct {
	service *tenantsrv.TenantService
}

func NewPlatformTenantHandlers(service *tenantsrv.TenantService) *PlatformTenantHandlers {
	return &PlatformTenantHandlers{service: service}
}

func (h *PlatformTenantHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	tenants := router.Group("/admin/tenants", authMiddleware.Authenticate())

	tenants.Post("/", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsWrite), h.CreateTenant)
	tenants.Get("/", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsRead), h.GetAllTenants)
	tenants.Get("/:id", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsRead), h.GetTenant)
	tenants.Put("/:id", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsWrite), h.UpdateTenant)
	tenants.Delete("/:id", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsDelete), h.DeleteTenant)
	tenants.Post("/:id/suspend", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsSuspend), h.SuspendTenant)
	tenants.Post("/:id/activate", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsSuspend), h.ActivateTenant)
	tenants.Post("/:id/upgrade", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsWrite), h.UpgradePlan)
	tenants.Get("/:id/stats", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsRead), h.GetTenantStats)
	tenants.Get("/:id/usage", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsRead), h.GetTenantUsage)
	tenants.Get("/:id/users", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsRead), h.GetTenantUsers)
	tenants.Get("/:id/config", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsConfig), h.GetTenantConfig)
	tenants.Put("/:id/config", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsConfig), h.SetTenantConfig)
	tenants.Delete("/:id/config/:key", authMiddleware.RequireScope(iamscopes.ScopePlatformTenantsConfig), h.DeleteTenantConfig)
}

func (h *PlatformTenantHandlers) CreateTenant(c *fiber.Ctx) error {
	req, err := kernel.BindAndValidate[tenant.CreateTenantRequest](c)
	if err != nil {
		return err
	}

	t, err := h.service.CreateTenant(c.Context(), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(t.ToDTO())
}

func (h *PlatformTenantHandlers) GetAllTenants(c *fiber.Ctx) error {
	response, err := h.service.GetAllTenants(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(response.ToDTO())
}

func (h *PlatformTenantHandlers) GetTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	response, err := h.service.GetTenantByID(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(response.ToDTO())
}

func (h *PlatformTenantHandlers) UpdateTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	req, err := kernel.BindAndValidate[tenant.UpdateTenantRequest](c)
	if err != nil {
		return err
	}

	updated, err := h.service.UpdateTenant(c.Context(), tenantID, req)
	if err != nil {
		return err
	}
	return c.JSON(updated.ToDTO())
}

func (h *PlatformTenantHandlers) DeleteTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	if err := h.service.DeleteTenant(c.Context(), tenantID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Tenant deleted successfully"})
}

func (h *PlatformTenantHandlers) SuspendTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	req, err := kernel.BindAndValidate[tenant.SuspendTenantRequest](c)
	if err != nil {
		return err
	}

	if err := h.service.SuspendTenant(c.Context(), tenantID, req.Reason); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Tenant suspended successfully"})
}

func (h *PlatformTenantHandlers) ActivateTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	if err := h.service.ActivateTenant(c.Context(), tenantID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Tenant activated successfully"})
}

func (h *PlatformTenantHandlers) UpgradePlan(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	req, err := kernel.BindAndValidate[tenant.UpgradePlanRequest](c)
	if err != nil {
		return err
	}

	if err := h.service.UpgradeTenantPlan(c.Context(), tenantID, req.NewPlan); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Plan upgraded successfully"})
}

func (h *PlatformTenantHandlers) GetTenantStats(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	stats, err := h.service.GetTenantStats(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(stats)
}

func (h *PlatformTenantHandlers) GetTenantUsage(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	usage, err := h.service.GetTenantUsage(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(usage)
}

func (h *PlatformTenantHandlers) GetTenantUsers(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	users, err := h.service.GetTenantUsers(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": users, "total": len(users)})
}

func (h *PlatformTenantHandlers) GetTenantConfig(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	config, err := h.service.GetTenantConfig(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(config)
}

func (h *PlatformTenantHandlers) SetTenantConfig(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	req, err := kernel.BindAndValidate[tenant.SetConfigRequest](c)
	if err != nil {
		return err
	}

	if err := h.service.SetTenantConfig(c.Context(), tenantID, req.Key, req.Value); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config saved successfully"})
}

func (h *PlatformTenantHandlers) DeleteTenantConfig(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	key := c.Params("key")

	if err := h.service.DeleteTenantConfig(c.Context(), tenantID, key); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config deleted successfully"})
}

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

	response, err := h.service.GetTenantByID(c.Context(), authContext.TenantID)
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

	stats, err := h.service.GetTenantStats(c.Context(), authContext.TenantID)
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

	usage, err := h.service.GetTenantUsage(c.Context(), authContext.TenantID)
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

	config, err := h.service.GetTenantConfig(c.Context(), authContext.TenantID)
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

	if err := h.service.SetTenantConfig(c.Context(), authContext.TenantID, req.Key, req.Value); err != nil {
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
	if err := h.service.DeleteTenantConfig(c.Context(), authContext.TenantID, key); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config deleted successfully"})
}

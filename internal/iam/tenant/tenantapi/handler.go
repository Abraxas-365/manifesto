package tenantapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantsrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type TenantHandlers struct {
	service *tenantsrv.TenantService
}

func NewTenantHandlers(service *tenantsrv.TenantService) *TenantHandlers {
	return &TenantHandlers{service: service}
}

func (h *TenantHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	tenants := router.Group("/tenants", authMiddleware.Authenticate())

	tenants.Post("/", authMiddleware.RequireScope("tenants:write"), h.CreateTenant)
	tenants.Get("/", authMiddleware.RequireScope("tenants:read"), h.GetAllTenants)
	tenants.Get("/:id", authMiddleware.RequireScope("tenants:read"), h.GetTenant)
	tenants.Put("/:id", authMiddleware.RequireScope("tenants:write"), h.UpdateTenant)
	tenants.Delete("/:id", authMiddleware.RequireScope("tenants:delete"), h.DeleteTenant)
	tenants.Post("/:id/suspend", authMiddleware.RequireScope("tenants:write"), h.SuspendTenant)
	tenants.Post("/:id/activate", authMiddleware.RequireScope("tenants:write"), h.ActivateTenant)
	tenants.Post("/:id/upgrade", authMiddleware.RequireScope("tenants:write"), h.UpgradePlan)
	tenants.Get("/:id/stats", authMiddleware.RequireScope("tenants:read"), h.GetTenantStats)
	tenants.Get("/:id/usage", authMiddleware.RequireScope("tenants:read"), h.GetTenantUsage)
	tenants.Get("/:id/users", authMiddleware.RequireScope("tenants:read"), h.GetTenantUsers)

	// Tenant config
	tenants.Get("/:id/config", authMiddleware.RequireScope("tenants:config"), h.GetTenantConfig)
	tenants.Put("/:id/config", authMiddleware.RequireScope("tenants:config"), h.SetTenantConfig)
	tenants.Delete("/:id/config/:key", authMiddleware.RequireScope("tenants:config"), h.DeleteTenantConfig)
}

func (h *TenantHandlers) CreateTenant(c *fiber.Ctx) error {
	var req tenant.CreateTenantRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	t, err := h.service.CreateTenant(c.Context(), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(t.ToDTO())
}

func (h *TenantHandlers) GetAllTenants(c *fiber.Ctx) error {
	response, err := h.service.GetAllTenants(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(response.ToDTO())
}

func (h *TenantHandlers) GetTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	response, err := h.service.GetTenantByID(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(response.ToDTO())
}

func (h *TenantHandlers) UpdateTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	var req tenant.UpdateTenantRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	updated, err := h.service.UpdateTenant(c.Context(), tenantID, req)
	if err != nil {
		return err
	}
	return c.JSON(updated.ToDTO())
}

func (h *TenantHandlers) DeleteTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	if err := h.service.DeleteTenant(c.Context(), tenantID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Tenant deleted successfully"})
}

func (h *TenantHandlers) SuspendTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.SuspendTenant(c.Context(), tenantID, req.Reason); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Tenant suspended successfully"})
}

func (h *TenantHandlers) ActivateTenant(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	if err := h.service.ActivateTenant(c.Context(), tenantID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Tenant activated successfully"})
}

func (h *TenantHandlers) UpgradePlan(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	var req tenant.UpgradePlanRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.UpgradeTenantPlan(c.Context(), tenantID, req.NewPlan); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Plan upgraded successfully"})
}

func (h *TenantHandlers) GetTenantStats(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	stats, err := h.service.GetTenantStats(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(stats)
}

func (h *TenantHandlers) GetTenantUsage(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	usage, err := h.service.GetTenantUsage(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(usage)
}

func (h *TenantHandlers) GetTenantUsers(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	users, err := h.service.GetTenantUsers(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": users, "total": len(users)})
}

func (h *TenantHandlers) GetTenantConfig(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	config, err := h.service.GetTenantConfig(c.Context(), tenantID)
	if err != nil {
		return err
	}
	return c.JSON(config)
}

func (h *TenantHandlers) SetTenantConfig(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	var req tenant.SetConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.SetTenantConfig(c.Context(), tenantID, req.Key, req.Value); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config saved successfully"})
}

func (h *TenantHandlers) DeleteTenantConfig(c *fiber.Ctx) error {
	tenantID := kernel.NewTenantID(c.Params("id"))
	key := c.Params("key")

	if err := h.service.DeleteTenantConfig(c.Context(), tenantID, key); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Config deleted successfully"})
}

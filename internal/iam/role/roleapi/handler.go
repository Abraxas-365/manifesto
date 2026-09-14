package roleapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/iam/role/rolesrv"
	iamscopes "github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type RoleHandlers struct {
	service *rolesrv.RoleService
}

func NewRoleHandlers(service *rolesrv.RoleService) *RoleHandlers {
	return &RoleHandlers{service: service}
}

func (h *RoleHandlers) RegisterRoutes(router fiber.Router, middleware *auth.UnifiedAuthMiddleware) {
	roles := router.Group("/roles", middleware.Authenticate())
	roles.Get("/", middleware.RequireScope(iamscopes.ScopeRolesRead), h.ListTenantRoles)
	roles.Post("/", middleware.RequireScope(iamscopes.ScopeRolesWrite), h.CreateRole)
	roles.Get("/:id", middleware.RequireScope(iamscopes.ScopeRolesRead), h.GetRole)
	roles.Put("/:id", middleware.RequireScope(iamscopes.ScopeRolesWrite), h.UpdateRole)
	roles.Delete("/:id", middleware.RequireScope(iamscopes.ScopeRolesDelete), h.DeleteRole)
	roles.Post("/:id/users", middleware.RequireScope(iamscopes.ScopeRolesAssign), h.AssignRoleToUser)
	roles.Delete("/:id/users/:userID", middleware.RequireScope(iamscopes.ScopeRolesAssign), h.UnassignRoleFromUser)

	router.Get("/users/:id/roles", middleware.Authenticate(), middleware.RequireScope(iamscopes.ScopeRolesRead), h.GetUserRoles)
}

func (h *RoleHandlers) CreateRole(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[role.CreateRoleRequest](c)
	if err != nil {
		return err
	}
	result, err := h.service.CreateRole(c.Context(), actor, actor.TenantID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(result.ToDTO())
}

func (h *RoleHandlers) ListTenantRoles(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetTenantRoles(c.Context(), actor, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *RoleHandlers) GetRole(c *fiber.Ctx) error {
	roleID := kernel.NewRoleID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetRoleByID(c.Context(), actor, roleID, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *RoleHandlers) UpdateRole(c *fiber.Ctx) error {
	roleID := kernel.NewRoleID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[role.UpdateRoleRequest](c)
	if err != nil {
		return err
	}
	result, err := h.service.UpdateRole(c.Context(), actor, roleID, actor.TenantID, req)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *RoleHandlers) DeleteRole(c *fiber.Ctx) error {
	roleID := kernel.NewRoleID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.DeleteRole(c.Context(), actor, roleID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *RoleHandlers) AssignRoleToUser(c *fiber.Ctx) error {
	roleID := kernel.NewRoleID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[role.AssignRoleRequest](c)
	if err != nil {
		return err
	}
	if err := h.service.AssignRoleToUser(c.Context(), actor, roleID, req.UserID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *RoleHandlers) UnassignRoleFromUser(c *fiber.Ctx) error {
	roleID := kernel.NewRoleID(c.Params("id"))
	userID := kernel.NewUserID(c.Params("userID"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.UnassignRoleFromUser(c.Context(), actor, roleID, userID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *RoleHandlers) GetUserRoles(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetUserRoles(c.Context(), actor, userID, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

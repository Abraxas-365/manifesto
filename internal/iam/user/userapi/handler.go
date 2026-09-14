package userapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	iamscopes "github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/iam/user/usersrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type UserHandlers struct {
	service *usersrv.UserService
}

func NewUserHandlers(service *usersrv.UserService) *UserHandlers {
	return &UserHandlers{service: service}
}

func (h *UserHandlers) RegisterRoutes(router fiber.Router, middleware *auth.UnifiedAuthMiddleware) {
	users := router.Group("/users", middleware.Authenticate())
	users.Get("/", middleware.RequireScope(iamscopes.ScopeUsersRead), h.ListTenantUsers)
	users.Get("/:id", middleware.RequireScope(iamscopes.ScopeUsersRead), h.GetUser)
	users.Put("/:id", middleware.RequireScope(iamscopes.ScopeUsersWrite), h.UpdateUser)
	users.Post("/:id/suspend", middleware.RequireScope(iamscopes.ScopeUsersWrite), h.SuspendUser)
	users.Post("/:id/reinstate", middleware.RequireScope(iamscopes.ScopeUsersWrite), h.ReinstateUser)
	users.Delete("/:id", middleware.RequireScope(iamscopes.ScopeUsersDelete), h.DeleteUser)
	users.Get("/:id/scopes", middleware.RequireScope(iamscopes.ScopeScopesRead), h.GetUserScopes)
	users.Put("/:id/scopes", middleware.RequireScope(iamscopes.ScopeScopesWrite), h.SetUserScopes)
	users.Post("/:id/scopes", middleware.RequireScope(iamscopes.ScopeScopesAssign), h.AddScopesToUser)
	users.Delete("/:id/scopes", middleware.RequireScope(iamscopes.ScopeScopesAssign), h.RemoveScopesFromUser)
}

func (h *UserHandlers) ListTenantUsers(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetUsersByTenant(c.Context(), actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result.ToDTO())
}

func (h *UserHandlers) GetUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetUserByID(c.Context(), userID, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result.ToDTO())
}

func (h *UserHandlers) UpdateUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[user.UpdateUserRequest](c)
	if err != nil {
		return err
	}
	// Profile editing must not bypass the dedicated scope-management permission.
	if req.Scopes != nil && !actor.HasScope(iamscopes.ScopeScopesWrite) {
		return user.ErrInsufficientScopes()
	}
	req.TenantID = actor.TenantID
	result, err := h.service.UpdateUser(c.Context(), userID, req)
	if err != nil {
		return err
	}
	return c.JSON(result.ToDTO())
}

func (h *UserHandlers) DeleteUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.DeleteUser(c.Context(), userID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandlers) GetUserScopes(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetUserScopes(c.Context(), userID, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *UserHandlers) SetUserScopes(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[user.SetUserScopesRequest](c)
	if err != nil {
		return err
	}
	if err := h.service.SetUserScopes(c.Context(), userID, actor.TenantID, req.Scopes, actor.Scopes); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandlers) AddScopesToUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[user.ChangeUserScopesRequest](c)
	if err != nil {
		return err
	}
	if err := h.service.AddScopesToUser(c.Context(), userID, actor.TenantID, req.Scopes, actor.Scopes); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandlers) RemoveScopesFromUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[user.ChangeUserScopesRequest](c)
	if err != nil {
		return err
	}
	if err := h.service.RemoveScopesFromUser(c.Context(), userID, actor.TenantID, req.Scopes); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandlers) SuspendUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.SuspendUser(c.Context(), userID, actor.TenantID, "Suspended by tenant administrator"); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandlers) ReinstateUser(c *fiber.Ctx) error {
	userID := kernel.NewUserID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.ReinstateUser(c.Context(), userID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

package userapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
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

func (h *UserHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	users := router.Group("/users", authMiddleware.Authenticate())

	users.Post("/", authMiddleware.RequireScope("users:write"), h.CreateUser)
	users.Get("/", authMiddleware.RequireScope("users:read"), h.GetTenantUsers)
	users.Get("/:id", authMiddleware.RequireScope("users:read"), h.GetUser)
	users.Put("/:id", authMiddleware.RequireScope("users:write"), h.UpdateUser)
	users.Delete("/:id", authMiddleware.RequireScope("users:delete"), h.DeleteUser)
	users.Post("/:id/activate", authMiddleware.RequireScope("users:write"), h.ActivateUser)
	users.Post("/:id/suspend", authMiddleware.RequireScope("users:write"), h.SuspendUser)
}

func (h *UserHandlers) CreateUser(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	var req user.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	req.TenantID = authContext.TenantID

	newUser, err := h.service.CreateUser(c.Context(), req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(newUser.ToDTO())
}

func (h *UserHandlers) GetTenantUsers(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	response, err := h.service.GetUsersByTenant(c.Context(), authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(response)
}

func (h *UserHandlers) GetUser(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("id"))
	response, err := h.service.GetUserByID(c.Context(), userID, authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(response)
}

func (h *UserHandlers) UpdateUser(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("id"))
	var req user.UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	req.TenantID = authContext.TenantID

	updated, err := h.service.UpdateUser(c.Context(), userID, req)
	if err != nil {
		return err
	}
	return c.JSON(updated.ToDTO())
}

func (h *UserHandlers) DeleteUser(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("id"))
	if err := h.service.DeleteUser(c.Context(), userID, authContext.TenantID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "User deleted successfully"})
}

func (h *UserHandlers) ActivateUser(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("id"))
	if err := h.service.ActivateUser(c.Context(), userID, authContext.TenantID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "User activated successfully"})
}

func (h *UserHandlers) SuspendUser(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("id"))
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.SuspendUser(c.Context(), userID, authContext.TenantID, req.Reason); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "User suspended successfully"})
}

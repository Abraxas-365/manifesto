package userapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/iam/user/usersrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type ScopeHandlers struct {
	service *usersrv.UserService
}

func NewScopeHandlers(service *usersrv.UserService) *ScopeHandlers {
	return &ScopeHandlers{service: service}
}

func (h *ScopeHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	// System scopes (available scopes catalog)
	scopes := router.Group("/scopes", authMiddleware.Authenticate())
	scopes.Get("/", authMiddleware.RequireScope("scopes:read"), h.GetAllAvailableScopes)

	// User scope management
	userScopes := router.Group("/users/:userId/scopes", authMiddleware.Authenticate())
	userScopes.Get("/", authMiddleware.RequireScope("scopes:read"), h.GetUserScopes)
	userScopes.Put("/", authMiddleware.RequireScope("scopes:write"), h.SetUserScopes)
	userScopes.Post("/", authMiddleware.RequireScope("scopes:assign"), h.AddUserScopes)
	userScopes.Delete("/", authMiddleware.RequireScope("scopes:assign"), h.RemoveUserScopes)
}

// GetAllAvailableScopes returns all scopes defined in the system, grouped by category.
func (h *ScopeHandlers) GetAllAvailableScopes(c *fiber.Ctx) error {
	response := h.service.GetAllAvailableScopes()
	return c.JSON(response)
}

// GetUserScopes returns the direct scopes assigned to a user.
func (h *ScopeHandlers) GetUserScopes(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("userId"))
	response, err := h.service.GetUserScopes(c.Context(), userID, authContext.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(response)
}

// SetUserScopes replaces all direct scopes for a user.
func (h *ScopeHandlers) SetUserScopes(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("userId"))
	var req struct {
		Scopes []string `json:"scopes" validate:"required,min=1"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.SetUserScopes(c.Context(), userID, authContext.TenantID, req.Scopes); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Scopes updated successfully"})
}

// AddUserScopes adds scopes to a user without removing existing ones.
func (h *ScopeHandlers) AddUserScopes(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("userId"))
	var req user.AddScopesRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.AddScopesToUser(c.Context(), userID, authContext.TenantID, req.Scopes); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Scopes added successfully"})
}

// RemoveUserScopes removes specific scopes from a user.
func (h *ScopeHandlers) RemoveUserScopes(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	userID := kernel.UserID(c.Params("userId"))
	var req user.RemoveScopesRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if err := h.service.RemoveScopesFromUser(c.Context(), userID, authContext.TenantID, req.Scopes); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"message": "Scopes removed successfully"})
}



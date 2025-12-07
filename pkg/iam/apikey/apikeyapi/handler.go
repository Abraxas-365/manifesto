package apikeyapi

import (
	"github.com/Abraxas-365/manifesto/pkg/iam"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey/apikeysrv"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/gofiber/fiber/v2"
)

type APIKeyHandlers struct {
	service *apikeysrv.APIKeyService
}

func NewAPIKeyHandlers(service *apikeysrv.APIKeyService) *APIKeyHandlers {
	return &APIKeyHandlers{service: service}
}

func (h *APIKeyHandlers) RegisterRoutes(app *fiber.App, authMiddleware *auth.UnifiedAuthMiddleware) {
	keys := app.Group("/api-keys", authMiddleware.Authenticate())

	keys.Post("/", h.CreateAPIKey)
	keys.Get("/", h.GetTenantAPIKeys)
	keys.Get("/:id", h.GetAPIKey)
	keys.Put("/:id", h.UpdateAPIKey)
	keys.Post("/:id/revoke", h.RevokeAPIKey)
	keys.Delete("/:id", h.DeleteAPIKey)
}

func (h *APIKeyHandlers) CreateAPIKey(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	var req apikey.CreateAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	response, err := h.service.CreateAPIKey(c.Context(), authContext.TenantID, *authContext.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

func (h *APIKeyHandlers) GetTenantAPIKeys(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	response, err := h.service.GetTenantAPIKeys(c.Context(), authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(response)
}

func (h *APIKeyHandlers) GetAPIKey(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	keyID := c.Params("id")
	key, err := h.service.GetAPIKeyByID(c.Context(), keyID, authContext.TenantID)
	if err != nil {
		return err
	}

	return c.JSON(key)
}

func (h *APIKeyHandlers) UpdateAPIKey(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	keyID := c.Params("id")
	var req apikey.UpdateAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	key, err := h.service.UpdateAPIKey(c.Context(), keyID, authContext.TenantID, req)
	if err != nil {
		return err
	}

	return c.JSON(key)
}

func (h *APIKeyHandlers) RevokeAPIKey(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	keyID := c.Params("id")
	if err := h.service.RevokeAPIKey(c.Context(), keyID, authContext.TenantID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "API key revoked successfully"})
}

func (h *APIKeyHandlers) DeleteAPIKey(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	keyID := c.Params("id")
	if err := h.service.DeleteAPIKey(c.Context(), keyID, authContext.TenantID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "API key deleted successfully"})
}

package apikeyapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey/apikeysrv"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	iamscopes "github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type APIKeyHandlers struct {
	service *apikeysrv.APIKeyService
}

func NewAPIKeyHandlers(service *apikeysrv.APIKeyService) *APIKeyHandlers {
	return &APIKeyHandlers{service: service}
}

func (h *APIKeyHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	keys := router.Group("/api-keys", authMiddleware.Authenticate())

	keys.Post("/", authMiddleware.RequireScope(iamscopes.ScopeAPIKeysWrite), h.CreateAPIKey)
	keys.Get("/", authMiddleware.RequireScope(iamscopes.ScopeAPIKeysRead), h.GetTenantAPIKeys)
	keys.Get("/:id", authMiddleware.RequireScope(iamscopes.ScopeAPIKeysRead), h.GetAPIKey)
	keys.Put("/:id", authMiddleware.RequireScope(iamscopes.ScopeAPIKeysWrite), h.UpdateAPIKey)
	keys.Post("/:id/revoke", authMiddleware.RequireScope(iamscopes.ScopeAPIKeysRevoke), h.RevokeAPIKey)
	keys.Delete("/:id", authMiddleware.RequireScope(iamscopes.ScopeAPIKeysDelete), h.DeleteAPIKey)
}

func (h *APIKeyHandlers) CreateAPIKey(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	req, err := kernel.BindAndValidate[apikey.CreateAPIKeyRequest](c)
	if err != nil {
		return err
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

	keyID := kernel.NewAPIKeyID(c.Params("id"))
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

	keyID := kernel.NewAPIKeyID(c.Params("id"))
	req, err := kernel.BindAndValidate[apikey.UpdateAPIKeyRequest](c)
	if err != nil {
		return err
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

	keyID := kernel.NewAPIKeyID(c.Params("id"))
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

	keyID := kernel.NewAPIKeyID(c.Params("id"))
	if err := h.service.DeleteAPIKey(c.Context(), keyID, authContext.TenantID); err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "API key deleted successfully"})
}

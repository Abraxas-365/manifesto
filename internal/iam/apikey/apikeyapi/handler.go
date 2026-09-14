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

func (h *APIKeyHandlers) RegisterRoutes(router fiber.Router, middleware *auth.UnifiedAuthMiddleware) {
	keys := router.Group("/api-keys", middleware.Authenticate())
	keys.Get("/", middleware.RequireScope(iamscopes.ScopeAPIKeysRead), h.ListTenantAPIKeys)
	keys.Post("/", middleware.RequireScope(iamscopes.ScopeAPIKeysWrite), h.CreateAPIKey)
	keys.Get("/:id", middleware.RequireScope(iamscopes.ScopeAPIKeysRead), h.GetAPIKey)
	keys.Put("/:id", middleware.RequireScope(iamscopes.ScopeAPIKeysWrite), h.UpdateAPIKey)
	keys.Delete("/:id", middleware.RequireScope(iamscopes.ScopeAPIKeysDelete), h.DeleteAPIKey)
	keys.Post("/:id/revoke", middleware.RequireScope(iamscopes.ScopeAPIKeysRevoke), h.RevokeAPIKey)
}

func (h *APIKeyHandlers) CreateAPIKey(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	// Creation requires a human creator. A key's associated user is not its caller.
	userID, isUser := actor.Actor.UserID()
	if !isUser {
		return fiber.NewError(fiber.StatusForbidden, "API key creation requires an authenticated user")
	}
	req, err := kernel.BindAndValidate[apikey.CreateAPIKeyRequest](c)
	if err != nil {
		return err
	}
	result, err := h.service.CreateAPIKey(c.Context(), actor, actor.TenantID, userID, req)
	if err != nil {
		return err
	}
	// The raw secret is returned only by creation and must not be cached.
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(fiber.StatusCreated).JSON(result)
}

func (h *APIKeyHandlers) ListTenantAPIKeys(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetTenantAPIKeys(c.Context(), actor, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *APIKeyHandlers) GetAPIKey(c *fiber.Ctx) error {
	keyID := kernel.NewAPIKeyID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetAPIKeyByID(c.Context(), actor, keyID, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *APIKeyHandlers) UpdateAPIKey(c *fiber.Ctx) error {
	keyID := kernel.NewAPIKeyID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	req, err := kernel.BindAndValidate[apikey.UpdateAPIKeyRequest](c)
	if err != nil {
		return err
	}
	result, err := h.service.UpdateAPIKey(c.Context(), actor, keyID, actor.TenantID, req)
	if err != nil {
		return err
	}
	return c.JSON(result)
}

func (h *APIKeyHandlers) DeleteAPIKey(c *fiber.Ctx) error {
	keyID := kernel.NewAPIKeyID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.DeleteAPIKey(c.Context(), actor, keyID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *APIKeyHandlers) RevokeAPIKey(c *fiber.Ctx) error {
	keyID := kernel.NewAPIKeyID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.RevokeAPIKey(c.Context(), actor, keyID, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

package invitationapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation/invitationsrv"
	iamscopes "github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type InvitationHandlers struct {
	service *invitationsrv.InvitationService
}

func NewInvitationHandlers(service *invitationsrv.InvitationService) *InvitationHandlers {
	return &InvitationHandlers{service: service}
}

func (h *InvitationHandlers) RegisterRoutes(router fiber.Router, middleware *auth.UnifiedAuthMiddleware) {
	invitations := router.Group("/invitations", middleware.Authenticate())
	invitations.Post("/", middleware.RequireScope(iamscopes.ScopeInvitationsWrite), h.CreateInvitation)
	invitations.Get("/", middleware.RequireScope(iamscopes.ScopeInvitationsRead), h.ListTenantInvitations)
	invitations.Get("/:id", middleware.RequireScope(iamscopes.ScopeInvitationsRead), h.GetInvitation)
	invitations.Post("/:id/resend", middleware.RequireScope(iamscopes.ScopeInvitationsWrite), h.ResendInvitation)
	invitations.Post("/:id/revoke", middleware.RequireScope(iamscopes.ScopeInvitationsRevoke), h.RevokeInvitation)
	invitations.Delete("/:id", middleware.RequireScope(iamscopes.ScopeInvitationsDelete), h.DeleteInvitation)
}

func (h *InvitationHandlers) ResendInvitation(c *fiber.Ctx) error {
	id := kernel.NewInvitationID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.ResendInvitation(c.Context(), id, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *InvitationHandlers) CreateInvitation(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	userID, isUser := actor.Actor.UserID()
	if !isUser {
		return fiber.NewError(fiber.StatusForbidden, "Invitation creation requires an authenticated user")
	}
	req, err := kernel.BindAndValidate[invitation.CreateInvitationRequest](c)
	if err != nil {
		return err
	}
	result, err := h.service.CreateInvitation(c.Context(), actor.TenantID, userID, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(result.ToDTO())
}

func (h *InvitationHandlers) ListTenantInvitations(c *fiber.Ctx) error {
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetTenantInvitations(c.Context(), actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result.ToDTO())
}

func (h *InvitationHandlers) GetInvitation(c *fiber.Ctx) error {
	id := kernel.NewInvitationID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	result, err := h.service.GetInvitationByID(c.Context(), id, actor.TenantID)
	if err != nil {
		return err
	}
	return c.JSON(result.ToDTO())
}

func (h *InvitationHandlers) RevokeInvitation(c *fiber.Ctx) error {
	id := kernel.NewInvitationID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.RevokeInvitation(c.Context(), id, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *InvitationHandlers) DeleteInvitation(c *fiber.Ctx) error {
	id := kernel.NewInvitationID(c.Params("id"))
	actor, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}
	if err := h.service.DeleteInvitation(c.Context(), id, actor.TenantID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

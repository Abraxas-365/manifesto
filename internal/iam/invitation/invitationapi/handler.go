package invitationapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation/invitationsrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

// InvitationHandlers handles invitation routes with Fiber
type InvitationHandlers struct {
	service *invitationsrv.InvitationService
}

// NewInvitationHandlers creates a new invitation handler
func NewInvitationHandlers(service *invitationsrv.InvitationService) *InvitationHandlers {
	return &InvitationHandlers{
		service: service,
	}
}

// RegisterRoutes registers the invitation routes in Fiber
func (h *InvitationHandlers) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	invitations := router.Group("/invitations", authMiddleware.Authenticate())

	// Protected routes
	invitations.Post("/", authMiddleware.RequireScope("invitations:write"), h.CreateInvitation)
	invitations.Get("/", authMiddleware.RequireScope("invitations:read"), h.GetTenantInvitations)
	invitations.Get("/pending", authMiddleware.RequireScope("invitations:read"), h.GetPendingInvitations)
	invitations.Get("/:id", authMiddleware.RequireScope("invitations:read"), h.GetInvitationByID)
	invitations.Delete("/:id", authMiddleware.RequireScope("invitations:delete"), h.DeleteInvitation)
	invitations.Post("/:id/revoke", authMiddleware.RequireScope("invitations:revoke"), h.RevokeInvitation)

	// Public routes
	public := router.Group("/invitations/public")
	public.Get("/validate", h.ValidateInvitationToken)
	public.Get("/token/:token", h.GetInvitationByToken)
}

// CreateInvitation creates a new invitation
func (h *InvitationHandlers) CreateInvitation(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	req, err := kernel.BindAndValidate[invitation.CreateInvitationRequest](c)
	if err != nil {
		return err
	}

	if authContext.UserID == nil {
		return iam.ErrUnauthorized()
	}

	// Create invitation
	inv, err := h.service.CreateInvitation(c.Context(), authContext.TenantID, *authContext.UserID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(inv.ToDTO())
}

// GetTenantInvitations gets all invitations for the tenant
func (h *InvitationHandlers) GetTenantInvitations(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	invitations, err := h.service.GetTenantInvitations(c.Context(), authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(invitations.ToDTO())
}

// GetPendingInvitations gets pending invitations for the tenant
func (h *InvitationHandlers) GetPendingInvitations(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	invitations, err := h.service.GetPendingInvitations(c.Context(), authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(invitations.ToDTO())
}

// GetInvitationByID gets an invitation by ID
func (h *InvitationHandlers) GetInvitationByID(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	invitationID := c.Params("id")
	if invitationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invitation_id is required",
		})
	}

	invitation, err := h.service.GetInvitationByID(c.Context(), invitationID, authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(invitation.ToDTO())
}

// GetInvitationByToken gets an invitation by token (public)
func (h *InvitationHandlers) GetInvitationByToken(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "token is required",
		})
	}

	invitation, err := h.service.GetInvitationByToken(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(invitation.ToDTO())
}

// ValidateInvitationToken validates an invitation token (public)
func (h *InvitationHandlers) ValidateInvitationToken(c *fiber.Ctx) error {
	token := c.Query("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "token is required",
		})
	}

	response, err := h.service.ValidateInvitationToken(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(response)
}

// RevokeInvitation revokes an invitation
func (h *InvitationHandlers) RevokeInvitation(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	invitationID := c.Params("id")
	if invitationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invitation_id is required",
		})
	}

	err := h.service.RevokeInvitation(c.Context(), invitationID, authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Invitation revoked successfully",
	})
}

// DeleteInvitation deletes an invitation
func (h *InvitationHandlers) DeleteInvitation(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	invitationID := c.Params("id")
	if invitationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invitation_id is required",
		})
	}

	err := h.service.DeleteInvitation(c.Context(), invitationID, authContext.TenantID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"message": "Invitation deleted successfully",
	})
}

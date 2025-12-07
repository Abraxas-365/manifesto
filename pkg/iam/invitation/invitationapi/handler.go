package invitationapi

import (
	"github.com/Abraxas-365/manifesto/pkg/iam"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation/invitationsrv"
	"github.com/gofiber/fiber/v2"
)

// InvitationHandlers maneja las rutas de invitaciones con Fiber
type InvitationHandlers struct {
	service *invitationsrv.InvitationService
}

// NewInvitationHandlers crea un nuevo handler de invitaciones
func NewInvitationHandlers(service *invitationsrv.InvitationService) *InvitationHandlers {
	return &InvitationHandlers{
		service: service,
	}
}

// RegisterRoutes registra las rutas de invitaciones en Fiber
func (h *InvitationHandlers) RegisterRoutes(app *fiber.App, authMiddleware *auth.UnifiedAuthMiddleware) {
	invitations := app.Group("/invitations", authMiddleware.Authenticate())

	// Rutas protegidas (requieren autenticación)
	invitations.Post("/", h.CreateInvitation)            // Crear invitación
	invitations.Get("/", h.GetTenantInvitations)         // Listar invitaciones del tenant
	invitations.Get("/pending", h.GetPendingInvitations) // Listar invitaciones pendientes
	invitations.Get("/:id", h.GetInvitationByID)         // Obtener invitación por ID
	invitations.Delete("/:id", h.DeleteInvitation)       // Eliminar invitación
	invitations.Post("/:id/revoke", h.RevokeInvitation)  // Revocar invitación

	// Rutas públicas (no requieren autenticación)
	public := app.Group("/invitations/public")
	public.Get("/validate", h.ValidateInvitationToken)  // Validar token
	public.Get("/token/:token", h.GetInvitationByToken) // Obtener invitación por token
}

// CreateInvitation crea una nueva invitación
func (h *InvitationHandlers) CreateInvitation(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Unauthorized",
		})
	}

	var req invitation.CreateInvitationRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if authContext.UserID == nil {
		return iam.ErrUnauthorized()
	}

	// Crear invitación
	inv, err := h.service.CreateInvitation(c.Context(), authContext.TenantID, *authContext.UserID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(inv.ToDTO())
}

// GetTenantInvitations obtiene todas las invitaciones del tenant
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

// GetPendingInvitations obtiene invitaciones pendientes del tenant
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

// GetInvitationByID obtiene una invitación por ID
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

// GetInvitationByToken obtiene una invitación por token (público)
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

// ValidateInvitationToken valida un token de invitación (público)
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

// RevokeInvitation revoca una invitación
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

// DeleteInvitation elimina una invitación
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

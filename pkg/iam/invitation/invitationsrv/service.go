// pkg/iam/invitation/invitationsrv/service.go
package invitationsrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation"
	"github.com/Abraxas-365/manifesto/pkg/iam/scopes"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant"
	"github.com/Abraxas-365/manifesto/pkg/iam/user"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/google/uuid"
)

// InvitationService proporciona operaciones de negocio para invitaciones
type InvitationService struct {
	invitationRepo invitation.InvitationRepository
	userRepo       user.UserRepository
	tenantRepo     tenant.TenantRepository
	config         *config.InvitationConfig
}

// NewInvitationService crea una nueva instancia del servicio de invitaciones
func NewInvitationService(
	invitationRepo invitation.InvitationRepository,
	userRepo user.UserRepository,
	tenantRepo tenant.TenantRepository,
	cfg *config.InvitationConfig,
) *InvitationService {
	return &InvitationService{
		invitationRepo: invitationRepo,
		userRepo:       userRepo,
		tenantRepo:     tenantRepo,
		config:         cfg,
	}
}

// CreateInvitation crea una nueva invitación
func (s *InvitationService) CreateInvitation(ctx context.Context, tenantID kernel.TenantID, invitedBy kernel.UserID, req invitation.CreateInvitationRequest) (*invitation.Invitation, error) {
	// Verificar que el tenant existe
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Verificar que el tenant está activo
	if !tenantEntity.IsActive() {
		return nil, tenant.ErrTenantSuspended()
	}

	// Verificar que el usuario que invita existe
	inviterUser, err := s.userRepo.FindByID(ctx, invitedBy, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Verificar que el invitador tiene permisos (admin o users:invite)
	if !inviterUser.IsAdmin() && !inviterUser.HasScope(scopes.ScopeUsersInvite) {
		return nil, errx.New("insufficient permissions to invite users", errx.TypeAuthorization).
			WithDetail("required_scope", scopes.ScopeUsersInvite)
	}

	// Verificar que el usuario no existe ya en el tenant
	existingUser, err := s.userRepo.FindByEmail(ctx, req.Email, tenantID)
	if err == nil && existingUser != nil {
		return nil, invitation.ErrUserAlreadyExists().WithDetail("email", req.Email)
	}

	// Verificar que no existe una invitación pendiente para este email
	exists, err := s.invitationRepo.ExistsPendingForEmail(ctx, req.Email, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to check pending invitation", errx.TypeInternal)
	}
	if exists {
		return nil, invitation.ErrInvitationAlreadyExists().WithDetail("email", req.Email)
	}

	// Determinar scopes
	resolvedScopes, err := s.resolveScopes(req)
	if err != nil {
		return nil, err
	}

	// Validar scopes
	if err := s.validateScopes(resolvedScopes); err != nil {
		return nil, err
	}

	// Generar token único usando configuración
	token, err := invitation.GenerateInvitationToken(s.config.TokenByteLength)
	if err != nil {
		return nil, errx.Wrap(err, "failed to generate invitation token", errx.TypeInternal)
	}

	// Calcular fecha de expiración usando configuración
	expiresIn := s.config.DefaultExpirationDays
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		expiresIn = *req.ExpiresIn
	}
	expiresAt := invitation.CalculateExpirationDate(expiresIn, s.config.DefaultExpirationDays)

	// Crear invitación
	newInvitation := &invitation.Invitation{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		Email:     req.Email,
		Token:     token,
		Scopes:    resolvedScopes,
		Status:    invitation.InvitationStatusPending,
		InvitedBy: invitedBy,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Guardar invitación
	if err := s.invitationRepo.Save(ctx, *newInvitation); err != nil {
		return nil, errx.Wrap(err, "failed to save invitation", errx.TypeInternal)
	}

	// TODO: Aquí deberías enviar un email con el link de invitación
	// Por ejemplo: https://yourapp.com/accept-invitation?token={token}

	return newInvitation, nil
}

// GetInvitationByID obtiene una invitación por ID
func (s *InvitationService) GetInvitationByID(ctx context.Context, invitationID string, tenantID kernel.TenantID) (*invitation.InvitationResponse, error) {
	inv, err := s.invitationRepo.FindByID(ctx, invitationID)
	if err != nil {
		return nil, invitation.ErrInvitationNotFound()
	}

	// Verificar que la invitación pertenece al tenant
	if inv.TenantID != tenantID {
		return nil, invitation.ErrInvitationNotFound()
	}

	return s.buildInvitationResponse(inv), nil
}

// GetInvitationByToken obtiene una invitación por token
func (s *InvitationService) GetInvitationByToken(ctx context.Context, token string) (*invitation.InvitationResponse, error) {
	inv, err := s.invitationRepo.FindByToken(ctx, token)
	if err != nil {
		return nil, invitation.ErrInvitationNotFound()
	}

	return s.buildInvitationResponse(inv), nil
}

// ValidateInvitationToken valida un token de invitación sin aceptarlo
func (s *InvitationService) ValidateInvitationToken(ctx context.Context, token string) (*invitation.ValidateInvitationResponse, error) {
	inv, err := s.invitationRepo.FindByToken(ctx, token)
	if err != nil {
		return &invitation.ValidateInvitationResponse{
			Valid:   false,
			Message: "Invitation not found",
		}, nil
	}

	if !inv.CanBeAccepted() {
		message := "Invalid invitation"
		if inv.IsExpired() {
			message = "Invitation expired"
		} else if inv.Status == invitation.InvitationStatusAccepted {
			message = "Invitation already accepted"
		} else if inv.Status == invitation.InvitationStatusRevoked {
			message = "Invitation revoked"
		}

		return &invitation.ValidateInvitationResponse{
			Valid:   false,
			Message: message,
		}, nil
	}

	invDTO := inv.ToDTO()
	return &invitation.ValidateInvitationResponse{
		Valid:      true,
		Invitation: &invDTO,
		Message:    "Valid invitation",
	}, nil
}

// GetTenantInvitations obtiene todas las invitaciones de un tenant
func (s *InvitationService) GetTenantInvitations(ctx context.Context, tenantID kernel.TenantID) (*invitation.InvitationListResponse, error) {
	// Verificar que el tenant existe
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	invitations, err := s.invitationRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get tenant invitations", errx.TypeInternal)
	}

	var responses []invitation.InvitationResponse
	for _, inv := range invitations {
		responses = append(responses, *s.buildInvitationResponse(inv))
	}

	return &invitation.InvitationListResponse{
		Invitations: responses,
		Total:       len(responses),
	}, nil
}

// GetPendingInvitations obtiene invitaciones pendientes de un tenant
func (s *InvitationService) GetPendingInvitations(ctx context.Context, tenantID kernel.TenantID) (*invitation.InvitationListResponse, error) {
	// Verificar que el tenant existe
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	invitations, err := s.invitationRepo.FindPendingByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get pending invitations", errx.TypeInternal)
	}

	var responses []invitation.InvitationResponse
	for _, inv := range invitations {
		responses = append(responses, *s.buildInvitationResponse(inv))
	}

	return &invitation.InvitationListResponse{
		Invitations: responses,
		Total:       len(responses),
	}, nil
}

// RevokeInvitation revoca una invitación
func (s *InvitationService) RevokeInvitation(ctx context.Context, invitationID string, tenantID kernel.TenantID) error {
	inv, err := s.invitationRepo.FindByID(ctx, invitationID)
	if err != nil {
		return invitation.ErrInvitationNotFound()
	}

	// Verificar que la invitación pertenece al tenant
	if inv.TenantID != tenantID {
		return invitation.ErrInvitationNotFound()
	}

	// Revocar invitación
	if err := inv.Revoke(); err != nil {
		return err
	}

	// Guardar cambios
	return s.invitationRepo.Save(ctx, *inv)
}

// DeleteInvitation elimina una invitación
func (s *InvitationService) DeleteInvitation(ctx context.Context, invitationID string, tenantID kernel.TenantID) error {
	inv, err := s.invitationRepo.FindByID(ctx, invitationID)
	if err != nil {
		return invitation.ErrInvitationNotFound()
	}

	// Verificar que la invitación pertenece al tenant
	if inv.TenantID != tenantID {
		return invitation.ErrInvitationNotFound()
	}

	// Solo se pueden eliminar invitaciones que no han sido aceptadas
	if inv.Status == invitation.InvitationStatusAccepted {
		return errx.New("cannot delete accepted invitation", errx.TypeBusiness)
	}

	return s.invitationRepo.Delete(ctx, invitationID)
}

// CleanupExpiredInvitations marca invitaciones expiradas
// Este método debería ser llamado por un cronjob
func (s *InvitationService) CleanupExpiredInvitations(ctx context.Context) (int, error) {
	expiredInvitations, err := s.invitationRepo.FindExpired(ctx)
	if err != nil {
		return 0, errx.Wrap(err, "failed to find expired invitations", errx.TypeInternal)
	}

	count := 0
	for _, inv := range expiredInvitations {
		inv.MarkAsExpired()
		if err := s.invitationRepo.Save(ctx, *inv); err != nil {
			// Log error but continue
			continue
		}
		count++
	}

	return count, nil
}

// GetAvailableScopeTemplates retorna las plantillas de scopes disponibles
func (s *InvitationService) GetAvailableScopeTemplates() []string {
	templates := make([]string, 0, len(scopes.ScopeGroups))
	for template := range scopes.ScopeGroups {
		templates = append(templates, template)
	}
	return templates
}

// ============================================================================
// Private Helper Methods
// ============================================================================

// resolveScopes determina los scopes finales basándose en la request
func (s *InvitationService) resolveScopes(req invitation.CreateInvitationRequest) ([]string, error) {
	// Si se proporcionan scopes directamente, usarlos
	if len(req.Scopes) > 0 {
		return req.Scopes, nil
	}

	// Si se proporciona un template, expandirlo
	if req.ScopeTemplate != nil && *req.ScopeTemplate != "" {
		scopeList := scopes.GetScopesByGroup(*req.ScopeTemplate)
		if len(scopeList) == 0 {
			return nil, invitation.ErrInvalidScopeTemplate().
				WithDetail("template", *req.ScopeTemplate).
				WithDetail("available_templates", s.GetAvailableScopeTemplates())
		}
		return scopeList, nil
	}

	// Default: usar template "viewer" o scopes básicos
	defaultScopes := scopes.GetScopesByGroup("viewer")
	return defaultScopes, nil
}

// validateScopes valida que los scopes sean válidos
func (s *InvitationService) validateScopes(scopesList []string) error {
	if len(scopesList) == 0 {
		return invitation.ErrInvalidScopes().WithDetail("reason", "at least one scope is required")
	}

	// Validar cada scope
	invalidScopes := []string{}
	for _, scope := range scopesList {
		if !scopes.ValidateScope(scope) {
			invalidScopes = append(invalidScopes, scope)
		}
	}

	if len(invalidScopes) > 0 {
		return invitation.ErrInvalidScopes().
			WithDetail("invalid_scopes", invalidScopes).
			WithDetail("hint", "Use GetAvailableScopeTemplates() to see valid options")
	}

	return nil
}

// buildInvitationResponse construye una respuesta completa
func (s *InvitationService) buildInvitationResponse(inv *invitation.Invitation) *invitation.InvitationResponse {
	return &invitation.InvitationResponse{
		Invitation:     *inv,
		ScopeTemplates: s.GetAvailableScopeTemplates(),
	}
}

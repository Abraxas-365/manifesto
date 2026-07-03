// pkg/iam/invitation/invitationsrv/service.go
package invitationsrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/google/uuid"
)

// InvitationService provides business operations for invitations
type InvitationService struct {
	invitationRepo      invitation.InvitationRepository
	userRepo            user.UserRepository
	tenantRepo          tenant.TenantRepository
	roleRepo            role.RoleRepository
	notificationService invitation.NotificationService
	config              *config.InvitationConfig
}

// NewInvitationService creates a new invitation service instance
func NewInvitationService(
	invitationRepo invitation.InvitationRepository,
	userRepo user.UserRepository,
	tenantRepo tenant.TenantRepository,
	roleRepo role.RoleRepository,
	notificationService invitation.NotificationService,
	cfg *config.InvitationConfig,
) *InvitationService {
	return &InvitationService{
		invitationRepo:      invitationRepo,
		userRepo:            userRepo,
		tenantRepo:          tenantRepo,
		roleRepo:            roleRepo,
		notificationService: notificationService,
		config:              cfg,
	}
}

// CreateInvitation creates a new invitation
func (s *InvitationService) CreateInvitation(ctx context.Context, tenantID kernel.TenantID, invitedBy kernel.UserID, req invitation.CreateInvitationRequest) (*invitation.Invitation, error) {
	// Check that the tenant exists
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Check that the tenant is active
	if !tenantEntity.IsActive() {
		return nil, tenant.ErrTenantSuspended()
	}

	// Check that the inviting user exists
	_, err = s.userRepo.FindByID(ctx, invitedBy, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Note: scope authorization is enforced by the API middleware (invitations:write)

	// Check that the user does not already exist in the tenant
	existingUser, err := s.userRepo.FindByEmail(ctx, req.Email, tenantID)
	if err == nil && existingUser != nil {
		return nil, invitation.ErrUserAlreadyExists().WithDetail("email", req.Email)
	}

	// Check that no pending invitation exists for this email
	exists, err := s.invitationRepo.ExistsPendingForEmail(ctx, req.Email, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to check pending invitation", errx.TypeInternal)
	}
	if exists {
		return nil, invitation.ErrInvitationAlreadyExists().WithDetail("email", req.Email)
	}

	// Validate role if provided
	if req.RoleID != nil && *req.RoleID != "" {
		_, err := s.roleRepo.FindByID(ctx, *req.RoleID, tenantID)
		if err != nil {
			return nil, role.ErrRoleNotFound().WithDetail("role_id", *req.RoleID)
		}
	}

	// Determine scopes
	resolvedScopes, err := s.resolveScopes(req)
	if err != nil {
		return nil, err
	}

	// Validate scopes
	if err := s.validateScopes(resolvedScopes); err != nil {
		return nil, err
	}

	// Generate unique token using configuration
	token, err := invitation.GenerateInvitationToken(s.config.TokenByteLength)
	if err != nil {
		return nil, errx.Wrap(err, "failed to generate invitation token", errx.TypeInternal)
	}

	// Calculate expiration date using configuration
	expiresIn := s.config.DefaultExpirationDays
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		expiresIn = *req.ExpiresIn
	}
	expiresAt := invitation.CalculateExpirationDate(expiresIn, s.config.DefaultExpirationDays)

	// Create invitation
	newInvitation := &invitation.Invitation{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		Email:     req.Email,
		Token:     token,
		Scopes:    resolvedScopes,
		RoleID:    req.RoleID,
		Status:    invitation.InvitationStatusPending,
		InvitedBy: invitedBy,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Save invitation
	if err := s.invitationRepo.Save(ctx, *newInvitation); err != nil {
		return nil, errx.Wrap(err, "failed to save invitation", errx.TypeInternal)
	}

	if s.notificationService != nil {
		if err := s.notificationService.SendInvitation(ctx, req.Email, token, tenantID, invitedBy); err != nil {
			return nil, errx.Wrap(err, "failed to send invitation email", errx.TypeExternal)
		}
	}

	return newInvitation, nil
}

// GetInvitationByID gets an invitation by ID
func (s *InvitationService) GetInvitationByID(ctx context.Context, invitationID string, tenantID kernel.TenantID) (*invitation.InvitationResponse, error) {
	inv, err := s.invitationRepo.FindByID(ctx, invitationID)
	if err != nil {
		return nil, invitation.ErrInvitationNotFound()
	}

	// Check that the invitation belongs to the tenant
	if inv.TenantID != tenantID {
		return nil, invitation.ErrInvitationNotFound()
	}

	return s.buildInvitationResponse(inv), nil
}

// GetInvitationByToken gets an invitation by token
func (s *InvitationService) GetInvitationByToken(ctx context.Context, token string) (*invitation.InvitationResponse, error) {
	inv, err := s.invitationRepo.FindByToken(ctx, token)
	if err != nil {
		return nil, invitation.ErrInvitationNotFound()
	}

	return s.buildInvitationResponse(inv), nil
}

// ValidateInvitationToken validates an invitation token without accepting it
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

// GetTenantInvitations gets all invitations for a tenant
func (s *InvitationService) GetTenantInvitations(ctx context.Context, tenantID kernel.TenantID) (*invitation.InvitationListResponse, error) {
	// Check that the tenant exists
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

// GetPendingInvitations gets pending invitations for a tenant
func (s *InvitationService) GetPendingInvitations(ctx context.Context, tenantID kernel.TenantID) (*invitation.InvitationListResponse, error) {
	// Check that the tenant exists
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

// RevokeInvitation revokes an invitation
func (s *InvitationService) RevokeInvitation(ctx context.Context, invitationID string, tenantID kernel.TenantID) error {
	inv, err := s.invitationRepo.FindByID(ctx, invitationID)
	if err != nil {
		return invitation.ErrInvitationNotFound()
	}

	// Check that the invitation belongs to the tenant
	if inv.TenantID != tenantID {
		return invitation.ErrInvitationNotFound()
	}

	// Revoke invitation
	if err := inv.Revoke(); err != nil {
		return err
	}

	// Save changes
	return s.invitationRepo.Save(ctx, *inv)
}

// DeleteInvitation deletes an invitation
func (s *InvitationService) DeleteInvitation(ctx context.Context, invitationID string, tenantID kernel.TenantID) error {
	inv, err := s.invitationRepo.FindByID(ctx, invitationID)
	if err != nil {
		return invitation.ErrInvitationNotFound()
	}

	// Check that the invitation belongs to the tenant
	if inv.TenantID != tenantID {
		return invitation.ErrInvitationNotFound()
	}

	// Only non-accepted invitations can be deleted
	if inv.Status == invitation.InvitationStatusAccepted {
		return errx.New("cannot delete accepted invitation", errx.TypeBusiness)
	}

	return s.invitationRepo.Delete(ctx, invitationID)
}

// CleanupExpiredInvitations marks expired invitations
// This method should be called by a cronjob
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

// ============================================================================
// Private Helper Methods
// ============================================================================

// resolveScopes determines the final scopes based on the request
func (s *InvitationService) resolveScopes(req invitation.CreateInvitationRequest) ([]string, error) {
	if len(req.Scopes) > 0 {
		return req.Scopes, nil
	}
	return []string{}, nil
}

// validateScopes validates that the scopes are valid
func (s *InvitationService) validateScopes(scopesList []string) error {
	if len(scopesList) == 0 {
		return invitation.ErrInvalidScopes().WithDetail("reason", "at least one scope is required")
	}

	// Validate each scope
	invalidScopes := []string{}
	for _, scope := range scopesList {
		if !scopes.ValidateScope(scope) {
			invalidScopes = append(invalidScopes, scope)
		}
	}

	if len(invalidScopes) > 0 {
		return invitation.ErrInvalidScopes().
			WithDetail("invalid_scopes", invalidScopes).
			WithDetail("hint", "Use GET /scopes to see valid scopes")
	}

	return nil
}

// buildInvitationResponse builds a complete response
func (s *InvitationService) buildInvitationResponse(inv *invitation.Invitation) *invitation.InvitationResponse {
	return &invitation.InvitationResponse{
		Invitation: *inv,
	}
}

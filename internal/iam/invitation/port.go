package invitation

import (
	"context"

	"github.com/Abraxas-365/manifesto/internal/kernel"
)

// NotificationService is a generic interface for sending invitation emails.
// Implementations can use notifx, console logging, or any other delivery mechanism.
type NotificationService interface {
	SendInvitation(ctx context.Context, email string, token string, tenantID kernel.TenantID, invitedBy kernel.UserID) error
}

// InvitationRepository defines the interface for invitation persistence
type InvitationRepository interface {
	// FindByID finds an invitation by ID
	FindByID(ctx context.Context, id kernel.InvitationID) (*Invitation, error)

	// FindByToken finds an invitation by token
	FindByToken(ctx context.Context, token string) (*Invitation, error)

	// FindByEmail finds invitations by email
	FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) ([]*Invitation, error)

	// FindPendingByEmail finds pending invitations for an email in a tenant
	FindPendingByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*Invitation, error)

	// FindByTenant finds all invitations for a tenant
	FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*Invitation, error)

	// FindPendingByTenant finds pending invitations for a tenant
	FindPendingByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*Invitation, error)

	// FindExpired finds expired invitations
	FindExpired(ctx context.Context) ([]*Invitation, error)

	// Save saves or updates an invitation
	Save(ctx context.Context, inv Invitation) error

	// Delete deletes an invitation
	Delete(ctx context.Context, id kernel.InvitationID) error

	// ExistsPendingForEmail checks whether a pending invitation exists for an email
	ExistsPendingForEmail(ctx context.Context, email string, tenantID kernel.TenantID) (bool, error)
}

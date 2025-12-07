package invitation

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

// InvitationRepository define el contrato para la persistencia de invitaciones
type InvitationRepository interface {
	// FindByID busca una invitación por ID
	FindByID(ctx context.Context, id string) (*Invitation, error)

	// FindByToken busca una invitación por token
	FindByToken(ctx context.Context, token string) (*Invitation, error)

	// FindByEmail busca invitaciones por email
	FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) ([]*Invitation, error)

	// FindPendingByEmail busca invitaciones pendientes para un email en un tenant
	FindPendingByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*Invitation, error)

	// FindByTenant busca todas las invitaciones de un tenant
	FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*Invitation, error)

	// FindPendingByTenant busca invitaciones pendientes de un tenant
	FindPendingByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*Invitation, error)

	// FindExpired busca invitaciones expiradas
	FindExpired(ctx context.Context) ([]*Invitation, error)

	// Save guarda o actualiza una invitación
	Save(ctx context.Context, inv Invitation) error

	// Delete elimina una invitación
	Delete(ctx context.Context, id string) error

	// ExistsPendingForEmail verifica si existe una invitación pendiente para un email
	ExistsPendingForEmail(ctx context.Context, email string, tenantID kernel.TenantID) (bool, error)
}

package role

import (
	"context"

	"github.com/Abraxas-365/manifesto/internal/kernel"
)

type RoleRepository interface {
	Save(ctx context.Context, r Role) error
	FindByID(ctx context.Context, id string, tenantID kernel.TenantID) (*Role, error)
	FindByName(ctx context.Context, name string, tenantID kernel.TenantID) (*Role, error)
	FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*Role, error)
	Delete(ctx context.Context, id string, tenantID kernel.TenantID) error

	// User-role assignments
	AssignToUser(ctx context.Context, userRole UserRole) error
	UnassignFromUser(ctx context.Context, userID kernel.UserID, roleID string, tenantID kernel.TenantID) error
	FindByUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) ([]*Role, error)
}

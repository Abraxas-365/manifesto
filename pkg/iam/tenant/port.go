package tenant

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

// TenantRepository define el contrato para la persistencia de tenants
type TenantRepository interface {
	FindByID(ctx context.Context, id kernel.TenantID) (*Tenant, error)
	FindAll(ctx context.Context) ([]*Tenant, error)
	FindActive(ctx context.Context) ([]*Tenant, error)
	Save(ctx context.Context, t Tenant) error
	Delete(ctx context.Context, id kernel.TenantID) error
}

// TenantConfigRepository define el contrato para configuraciones del tenant
type TenantConfigRepository interface {
	FindByTenant(ctx context.Context, tenantID kernel.TenantID) (map[string]string, error)
	SaveSetting(ctx context.Context, tenantID kernel.TenantID, key, value string) error
	DeleteSetting(ctx context.Context, tenantID kernel.TenantID, key string) error
}

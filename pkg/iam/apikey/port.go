package apikey

import (
	"context"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

type APIKeyRepository interface {
	Save(ctx context.Context, key APIKey) error
	FindByID(ctx context.Context, id string, tenantID kernel.TenantID) (*APIKey, error)
	FindByHash(ctx context.Context, keyHash string) (*APIKey, error)
	FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*APIKey, error)
	FindActiveByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*APIKey, error)
	FindByUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) ([]*APIKey, error)
	Delete(ctx context.Context, id string, tenantID kernel.TenantID) error
	UpdateLastUsed(ctx context.Context, id string) error
}

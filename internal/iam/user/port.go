package user

import (
	"context"

	"github.com/Abraxas-365/manifesto/internal/kernel"
)

// UserRepository defines the contract for user persistence
type UserRepository interface {
	FindByID(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) (*User, error)
	FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*User, error)
	FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*User, error)
	Save(ctx context.Context, u User) error
	Delete(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) error
	ExistsByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (bool, error)
	FindByEmailAcrossTenants(ctx context.Context, email string) ([]*User, error)
}

// PasswordService defines the contract for managing passwords
type PasswordService interface {
	HashPassword(password string) (string, error)
	VerifyPassword(hashedPassword, password string) bool
}

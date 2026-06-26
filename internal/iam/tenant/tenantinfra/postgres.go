package tenantinfra

import (
	"context"
	"database/sql"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/jmoiron/sqlx"
)

// PostgresTenantRepository is the PostgreSQL implementation of TenantRepository
type PostgresTenantRepository struct {
	db *sqlx.DB
}

// NewPostgresTenantRepository creates a new instance of the tenant repository
func NewPostgresTenantRepository(db *sqlx.DB) tenant.TenantRepository {
	return &PostgresTenantRepository{
		db: db,
	}
}

// FindByID finds a tenant by ID
func (r *PostgresTenantRepository) FindByID(ctx context.Context, id kernel.TenantID) (*tenant.Tenant, error) {
	query := `
		SELECT
			id, company_name, status, subscription_plan,
			max_users, current_users, trial_expires_at, subscription_expires_at,
			created_at, updated_at
		FROM tenants
		WHERE id = $1`

	var t tenant.Tenant
	err := r.db.GetContext(ctx, &t, query, id.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, tenant.ErrTenantNotFound().WithDetail("tenant_id", id.String())
		}
		return nil, errx.Wrap(err, "failed to find tenant by id", errx.TypeInternal).
			WithDetail("tenant_id", id.String())
	}

	return &t, nil
}

// FindAll finds all tenants
func (r *PostgresTenantRepository) FindAll(ctx context.Context) ([]*tenant.Tenant, error) {
	query := `
		SELECT
			id, company_name, status, subscription_plan,
			max_users, current_users, trial_expires_at, subscription_expires_at,
			created_at, updated_at
		FROM tenants
		ORDER BY company_name ASC`

	var tenants []tenant.Tenant
	err := r.db.SelectContext(ctx, &tenants, query)
	if err != nil {
		return nil, errx.Wrap(err, "failed to find all tenants", errx.TypeInternal)
	}

	// Convert to slice of pointers
	result := make([]*tenant.Tenant, len(tenants))
	for i := range tenants {
		result[i] = &tenants[i]
	}

	return result, nil
}

// FindActive finds all active tenants
func (r *PostgresTenantRepository) FindActive(ctx context.Context) ([]*tenant.Tenant, error) {
	query := `
		SELECT
			id, company_name, status, subscription_plan,
			max_users, current_users, trial_expires_at, subscription_expires_at,
			created_at, updated_at
		FROM tenants
		WHERE status = 'ACTIVE'
		ORDER BY company_name ASC`

	var tenants []tenant.Tenant
	err := r.db.SelectContext(ctx, &tenants, query)
	if err != nil {
		return nil, errx.Wrap(err, "failed to find active tenants", errx.TypeInternal)
	}

	// Convert to slice of pointers
	result := make([]*tenant.Tenant, len(tenants))
	for i := range tenants {
		result[i] = &tenants[i]
	}

	return result, nil
}

// Save saves or updates a tenant
func (r *PostgresTenantRepository) Save(ctx context.Context, t tenant.Tenant) error {
	// Check if the tenant already exists
	exists, err := r.tenantExists(ctx, t.ID)
	if err != nil {
		return errx.Wrap(err, "failed to check tenant existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, t)
	}
	return r.create(ctx, t)
}

// create creates a new tenant
func (r *PostgresTenantRepository) create(ctx context.Context, t tenant.Tenant) error {
	query := `
		INSERT INTO tenants (
			id, company_name, status, subscription_plan,
			max_users, current_users, trial_expires_at, subscription_expires_at,
			created_at, updated_at
		) VALUES (
			:id, :company_name, :status, :subscription_plan,
			:max_users, :current_users, :trial_expires_at, :subscription_expires_at,
			:created_at, :updated_at
		)`

	_, err := r.db.NamedExecContext(ctx, query, t)
	if err != nil {
		return errx.Wrap(err, "failed to create tenant", errx.TypeInternal).
			WithDetail("tenant_id", t.ID.String())
	}

	return nil
}

// update updates an existing tenant
func (r *PostgresTenantRepository) update(ctx context.Context, t tenant.Tenant) error {
	query := `
		UPDATE tenants SET
			company_name = :company_name,
			status = :status,
			subscription_plan = :subscription_plan,
			max_users = :max_users,
			current_users = :current_users,
			trial_expires_at = :trial_expires_at,
			subscription_expires_at = :subscription_expires_at,
			updated_at = :updated_at
		WHERE id = :id`

	result, err := r.db.NamedExecContext(ctx, query, t)
	if err != nil {
		return errx.Wrap(err, "failed to update tenant", errx.TypeInternal).
			WithDetail("tenant_id", t.ID.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return tenant.ErrTenantNotFound().WithDetail("tenant_id", t.ID.String())
	}

	return nil
}

// Delete deletes a tenant
func (r *PostgresTenantRepository) Delete(ctx context.Context, id kernel.TenantID) error {
	query := `DELETE FROM tenants WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id.String())
	if err != nil {
		return errx.Wrap(err, "failed to delete tenant", errx.TypeInternal).
			WithDetail("tenant_id", id.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return tenant.ErrTenantNotFound().WithDetail("tenant_id", id.String())
	}

	return nil
}

// tenantExists checks if a tenant exists by ID
func (r *PostgresTenantRepository) tenantExists(ctx context.Context, id kernel.TenantID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, id.String())
	if err != nil {
		return false, errx.Wrap(err, "failed to check tenant existence", errx.TypeInternal).
			WithDetail("tenant_id", id.String())
	}

	return exists, nil
}

// ============================================================================
// TenantConfigRepository Implementation
// ============================================================================

// PostgresTenantConfigRepository is the PostgreSQL implementation of TenantConfigRepository
type PostgresTenantConfigRepository struct {
	db *sqlx.DB
}

// NewPostgresTenantConfigRepository creates a new instance of the tenant configuration repository
func NewPostgresTenantConfigRepository(db *sqlx.DB) tenant.TenantConfigRepository {
	return &PostgresTenantConfigRepository{
		db: db,
	}
}

// FindByTenant finds all configuration for a tenant
func (r *PostgresTenantConfigRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) (map[string]string, error) {
	query := `
		SELECT config_key, config_value 
		FROM tenant_config 
		WHERE tenant_id = $1`

	rows, err := r.db.QueryContext(ctx, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find tenant config", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}
	defer rows.Close()

	config := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, errx.Wrap(err, "failed to scan tenant config", errx.TypeInternal)
		}
		config[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, errx.Wrap(err, "error iterating tenant config rows", errx.TypeInternal)
	}

	return config, nil
}

// SaveSetting saves a specific configuration setting for a tenant
func (r *PostgresTenantConfigRepository) SaveSetting(ctx context.Context, tenantID kernel.TenantID, key, value string) error {
	query := `
		INSERT INTO tenant_config (tenant_id, config_key, config_value, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (tenant_id, config_key) DO UPDATE
		SET config_value = EXCLUDED.config_value, updated_at = NOW()`

	_, err := r.db.ExecContext(ctx, query, tenantID.String(), key, value)
	if err != nil {
		return errx.Wrap(err, "failed to save tenant config setting", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String()).
			WithDetail("key", key)
	}

	return nil
}

// DeleteSetting deletes a specific configuration setting for a tenant
func (r *PostgresTenantConfigRepository) DeleteSetting(ctx context.Context, tenantID kernel.TenantID, key string) error {
	query := `DELETE FROM tenant_config WHERE tenant_id = $1 AND config_key = $2`

	result, err := r.db.ExecContext(ctx, query, tenantID.String(), key)
	if err != nil {
		return errx.Wrap(err, "failed to delete tenant config setting", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String()).
			WithDetail("key", key)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return errx.New("tenant config setting not found", errx.TypeNotFound).
			WithDetail("tenant_id", tenantID.String()).
			WithDetail("key", key)
	}

	return nil
}

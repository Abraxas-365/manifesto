package apikeyinfra

import (
	"context"
	"database/sql"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// PostgresAPIKeyRepository es la implementación en PostgreSQL de APIKeyRepository.
type PostgresAPIKeyRepository struct {
	db *sqlx.DB
}

// NewPostgresAPIKeyRepository crea una nueva instancia del repositorio.
func NewPostgresAPIKeyRepository(db *sqlx.DB) apikey.APIKeyRepository {
	return &PostgresAPIKeyRepository{
		db: db,
	}
}

// Save inserta o actualiza una APIKey.
func (r *PostgresAPIKeyRepository) Save(ctx context.Context, key apikey.APIKey) error {
	exists, err := r.keyExists(ctx, key.ID)
	if err != nil {
		return errx.Wrap(err, "failed to check API key existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, key)
	}
	return r.create(ctx, key)
}

func (r *PostgresAPIKeyRepository) create(ctx context.Context, key apikey.APIKey) error {
	query := `
		INSERT INTO api_keys (
			id, key_hash, key_prefix, tenant_id, user_id, name, description,
			scopes, is_active, expires_at, last_used_at, created_at, updated_at
		) VALUES (
			:id, :key_hash, :key_prefix, :tenant_id, :user_id, :name, :description,
			:scopes, :is_active, :expires_at, :last_used_at, :created_at, :updated_at
		)`

	keyWithPGArray := toPersistence(key)

	_, err := r.db.NamedExecContext(ctx, query, keyWithPGArray)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // unique_violation
			return apikey.ErrAPIKeyInvalid().WithDetail("reason", "key name or hash already exists")
		}
		return errx.Wrap(err, "failed to create API key", errx.TypeInternal).
			WithDetail("key_id", key.ID)
	}
	return nil
}

func (r *PostgresAPIKeyRepository) update(ctx context.Context, key apikey.APIKey) error {
	query := `
		UPDATE api_keys SET
			name = :name,
			description = :description,
			scopes = :scopes,
			is_active = :is_active,
			expires_at = :expires_at,
			last_used_at = :last_used_at,
			updated_at = :updated_at
		WHERE id = :id AND tenant_id = :tenant_id`

	keyWithPGArray := toPersistence(key)

	result, err := r.db.NamedExecContext(ctx, query, keyWithPGArray)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" { // unique_violation on name
			return apikey.ErrAPIKeyInvalid().WithDetail("reason", "key name already exists for this tenant")
		}
		return errx.Wrap(err, "failed to update API key", errx.TypeInternal).
			WithDetail("key_id", key.ID)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected on update", errx.TypeInternal)
	}
	if rowsAffected == 0 {
		return apikey.ErrAPIKeyNotFound()
	}

	return nil
}

// FindByID busca una API key por su ID y tenant ID.
func (r *PostgresAPIKeyRepository) FindByID(ctx context.Context, id string, tenantID kernel.TenantID) (*apikey.APIKey, error) {
	var key apiKeyPersistence
	query := `SELECT * FROM api_keys WHERE id = $1 AND tenant_id = $2`
	err := r.db.GetContext(ctx, &key, query, id, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apikey.ErrAPIKeyNotFound()
		}
		return nil, errx.Wrap(err, "failed to find API key by ID", errx.TypeInternal)
	}
	domainKey := toDomain(key)
	return &domainKey, nil
}

// FindByHash busca una API key por su hash SHA-256.
func (r *PostgresAPIKeyRepository) FindByHash(ctx context.Context, keyHash string) (*apikey.APIKey, error) {
	var key apiKeyPersistence
	query := `SELECT * FROM api_keys WHERE key_hash = $1`
	err := r.db.GetContext(ctx, &key, query, keyHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apikey.ErrAPIKeyNotFound()
		}
		return nil, errx.Wrap(err, "failed to find API key by hash", errx.TypeInternal)
	}
	domainKey := toDomain(key)
	return &domainKey, nil
}

// FindByTenant busca todas las API keys para un tenant.
func (r *PostgresAPIKeyRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*apikey.APIKey, error) {
	var keys []apiKeyPersistence
	query := `SELECT * FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &keys, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find API keys by tenant", errx.TypeInternal)
	}
	return toDomainSlice(keys), nil
}

// FindActiveByTenant busca todas las API keys activas de un tenant.
func (r *PostgresAPIKeyRepository) FindActiveByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*apikey.APIKey, error) {
	var keys []apiKeyPersistence
	query := `SELECT * FROM api_keys WHERE tenant_id = $1 AND is_active = true ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &keys, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find active API keys by tenant", errx.TypeInternal)
	}
	return toDomainSlice(keys), nil
}

// FindByUser busca todas las API keys para un usuario específico.
func (r *PostgresAPIKeyRepository) FindByUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) ([]*apikey.APIKey, error) {
	var keys []apiKeyPersistence
	query := `SELECT * FROM api_keys WHERE user_id = $1 AND tenant_id = $2 ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &keys, query, userID.String(), tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find API keys by user", errx.TypeInternal)
	}
	return toDomainSlice(keys), nil
}

// Delete elimina una API key de la base de datos.
func (r *PostgresAPIKeyRepository) Delete(ctx context.Context, id string, tenantID kernel.TenantID) error {
	query := `DELETE FROM api_keys WHERE id = $1 AND tenant_id = $2`
	result, err := r.db.ExecContext(ctx, query, id, tenantID.String())
	if err != nil {
		return errx.Wrap(err, "failed to delete API key", errx.TypeInternal)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected on delete", errx.TypeInternal)
	}
	if rowsAffected == 0 {
		return apikey.ErrAPIKeyNotFound()
	}
	return nil
}

// UpdateLastUsed actualiza el timestamp de último uso de una key.
func (r *PostgresAPIKeyRepository) UpdateLastUsed(ctx context.Context, id string) error {
	query := `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errx.Wrap(err, "failed to update last used time for API key", errx.TypeInternal)
	}
	return nil
}

func (r *PostgresAPIKeyRepository) keyExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM api_keys WHERE id = $1)`
	err := r.db.GetContext(ctx, &exists, query, id)
	if err != nil {
		return false, errx.Wrap(err, "failed to check key existence", errx.TypeInternal)
	}
	return exists, nil
}

// Struct auxiliar para persistencia que maneja tipos de DB específicos.
type apiKeyPersistence struct {
	ID          string          `db:"id"`
	KeyHash     string          `db:"key_hash"`
	KeyPrefix   string          `db:"key_prefix"`
	TenantID    kernel.TenantID `db:"tenant_id"`
	UserID      *kernel.UserID  `db:"user_id"`
	Name        string          `db:"name"`
	Description sql.NullString  `db:"description"`
	Scopes      pq.StringArray  `db:"scopes"`
	IsActive    bool            `db:"is_active"`
	ExpiresAt   *time.Time      `db:"expires_at"`
	LastUsedAt  *time.Time      `db:"last_used_at"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

// toPersistence convierte el modelo de dominio a un modelo de persistencia.
func toPersistence(key apikey.APIKey) apiKeyPersistence {
	return apiKeyPersistence{
		ID:          key.ID,
		KeyHash:     key.KeyHash,
		KeyPrefix:   key.KeyPrefix,
		TenantID:    key.TenantID,
		UserID:      key.UserID,
		Name:        key.Name,
		Description: sql.NullString{String: key.Description, Valid: key.Description != ""},
		Scopes:      key.Scopes,
		IsActive:    key.IsActive,
		ExpiresAt:   key.ExpiresAt,
		LastUsedAt:  key.LastUsedAt,
		CreatedAt:   key.CreatedAt,
		UpdatedAt:   key.UpdatedAt,
	}
}

// toDomain convierte el modelo de persistencia al modelo de dominio.
func toDomain(p apiKeyPersistence) apikey.APIKey {
	return apikey.APIKey{
		ID:          p.ID,
		KeyHash:     p.KeyHash,
		KeyPrefix:   p.KeyPrefix,
		TenantID:    p.TenantID,
		UserID:      p.UserID,
		Name:        p.Name,
		Description: p.Description.String,
		Scopes:      p.Scopes,
		IsActive:    p.IsActive,
		ExpiresAt:   p.ExpiresAt,
		LastUsedAt:  p.LastUsedAt,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

// toDomainSlice convierte un slice de persistencia a un slice de dominio.
func toDomainSlice(pKeys []apiKeyPersistence) []*apikey.APIKey {
	domainKeys := make([]*apikey.APIKey, len(pKeys))
	for i, p := range pKeys {
		k := toDomain(p)
		domainKeys[i] = &k
	}
	return domainKeys
}

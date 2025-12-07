package userinfra

import (
	"context"
	"database/sql"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/user"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// PostgresUserRepository implementación de PostgreSQL para UserRepository
type PostgresUserRepository struct {
	db *sqlx.DB
}

// NewPostgresUserRepository crea una nueva instancia del repositorio de usuarios
func NewPostgresUserRepository(db *sqlx.DB) user.UserRepository {
	return &PostgresUserRepository{
		db: db,
	}
}

// FindByID busca un usuario por ID y tenant
func (r *PostgresUserRepository) FindByID(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) (*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified,
			last_login_at, created_at, updated_at
		FROM users
		WHERE id = $1 AND tenant_id = $2`

	var u user.User
	err := r.db.GetContext(ctx, &u, query, id.String(), tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, user.ErrUserNotFound().WithDetail("user_id", id.String())
		}
		return nil, errx.Wrap(err, "failed to find user by id", errx.TypeInternal).
			WithDetail("user_id", id.String()).
			WithDetail("tenant_id", tenantID.String())
	}

	return &u, nil
}

// FindByEmail busca un usuario por email y tenant
func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified,
			last_login_at, created_at, updated_at
		FROM users
		WHERE email = $1 AND tenant_id = $2`

	var u user.User
	err := r.db.GetContext(ctx, &u, query, email, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, user.ErrUserNotFound().WithDetail("email", email)
		}
		return nil, errx.Wrap(err, "failed to find user by email", errx.TypeInternal).
			WithDetail("email", email).
			WithDetail("tenant_id", tenantID.String())
	}

	return &u, nil
}

// FindByTenant busca todos los usuarios de un tenant
func (r *PostgresUserRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified,
			last_login_at, created_at, updated_at
		FROM users
		WHERE tenant_id = $1
		ORDER BY name ASC`

	var users []user.User
	err := r.db.SelectContext(ctx, &users, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find users by tenant", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convertir a slice de punteros
	result := make([]*user.User, len(users))
	for i := range users {
		result[i] = &users[i]
	}

	return result, nil
}

// Save guarda o actualiza un usuario
func (r *PostgresUserRepository) Save(ctx context.Context, u user.User) error {
	// Verificar si el usuario ya existe
	exists, err := r.userExists(ctx, u.ID, u.TenantID)
	if err != nil {
		return errx.Wrap(err, "failed to check user existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, u)
	}
	return r.create(ctx, u)
}

// create crea un nuevo usuario
func (r *PostgresUserRepository) create(ctx context.Context, u user.User) error {
	query := `
		INSERT INTO users (
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified,
			last_login_at, created_at, updated_at
		) VALUES (
			:id, :tenant_id, :email, :name, :picture, :status, :scopes,
			:oauth_provider, :oauth_provider_id, :email_verified,
			:last_login_at, :created_at, :updated_at
		)`

	_, err := r.db.NamedExecContext(ctx, query, u)
	if err != nil {
		// Verificar violación de constraint de email único
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" && pqErr.Constraint == "users_email_tenant_id_key" {
				return user.ErrUserAlreadyExists().
					WithDetail("email", u.Email).
					WithDetail("tenant_id", u.TenantID.String())
			}
		}
		return errx.Wrap(err, "failed to create user", errx.TypeInternal).
			WithDetail("user_id", u.ID.String()).
			WithDetail("email", u.Email)
	}

	return nil
}

// update actualiza un usuario existente
func (r *PostgresUserRepository) update(ctx context.Context, u user.User) error {
	query := `
		UPDATE users SET
			email = :email,
			name = :name,
			picture = :picture,
			status = :status,
			scopes = :scopes,
			oauth_provider = :oauth_provider,
			oauth_provider_id = :oauth_provider_id,
			email_verified = :email_verified,
			last_login_at = :last_login_at,
			updated_at = :updated_at
		WHERE id = :id AND tenant_id = :tenant_id`

	result, err := r.db.NamedExecContext(ctx, query, u)
	if err != nil {
		// Verificar violación de constraint de email único
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" && pqErr.Constraint == "users_email_tenant_id_key" {
				return user.ErrUserAlreadyExists().
					WithDetail("email", u.Email).
					WithDetail("tenant_id", u.TenantID.String())
			}
		}
		return errx.Wrap(err, "failed to update user", errx.TypeInternal).
			WithDetail("user_id", u.ID.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return user.ErrUserNotFound().WithDetail("user_id", u.ID.String())
	}

	return nil
}

// Delete elimina un usuario
func (r *PostgresUserRepository) Delete(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) error {
	query := `DELETE FROM users WHERE id = $1 AND tenant_id = $2`

	result, err := r.db.ExecContext(ctx, query, id.String(), tenantID.String())
	if err != nil {
		return errx.Wrap(err, "failed to delete user", errx.TypeInternal).
			WithDetail("user_id", id.String()).
			WithDetail("tenant_id", tenantID.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return user.ErrUserNotFound().WithDetail("user_id", id.String())
	}

	return nil
}

// ExistsByEmail verifica si existe un usuario con el email dado en el tenant
func (r *PostgresUserRepository) ExistsByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1 AND tenant_id = $2)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, email, tenantID.String())
	if err != nil {
		return false, errx.Wrap(err, "failed to check user existence by email", errx.TypeInternal).
			WithDetail("email", email).
			WithDetail("tenant_id", tenantID.String())
	}

	return exists, nil
}

// userExists verifica si un usuario existe por ID y tenant
func (r *PostgresUserRepository) userExists(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND tenant_id = $2)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, id.String(), tenantID.String())
	if err != nil {
		return false, errx.Wrap(err, "failed to check user existence", errx.TypeInternal).
			WithDetail("user_id", id.String()).
			WithDetail("tenant_id", tenantID.String())
	}

	return exists, nil
}

// FindByStatus busca usuarios por estado (método adicional útil)
func (r *PostgresUserRepository) FindByStatus(ctx context.Context, status user.UserStatus, tenantID kernel.TenantID) ([]*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified,
			last_login_at, created_at, updated_at
		FROM users
		WHERE status = $1 AND tenant_id = $2
		ORDER BY name ASC`

	var users []user.User
	err := r.db.SelectContext(ctx, &users, query, status, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find users by status", errx.TypeInternal).
			WithDetail("status", string(status)).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convertir a slice de punteros
	result := make([]*user.User, len(users))
	for i := range users {
		result[i] = &users[i]
	}

	return result, nil
}

// FindActiveUsers busca usuarios activos (método adicional útil)
func (r *PostgresUserRepository) FindActiveUsers(ctx context.Context, tenantID kernel.TenantID) ([]*user.User, error) {
	return r.FindByStatus(ctx, user.UserStatusActive, tenantID)
}

// CountByTenant cuenta los usuarios de un tenant
func (r *PostgresUserRepository) CountByTenant(ctx context.Context, tenantID kernel.TenantID) (int, error) {
	query := `SELECT COUNT(*) FROM users WHERE tenant_id = $1`

	var count int
	err := r.db.GetContext(ctx, &count, query, tenantID.String())
	if err != nil {
		return 0, errx.Wrap(err, "failed to count users by tenant", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	return count, nil
}

// FindByOAuthProvider busca un usuario por proveedor OAuth y ID
func (r *PostgresUserRepository) FindByOAuthProvider(ctx context.Context, provider string, providerID string, tenantID kernel.TenantID) (*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified,
			last_login_at, created_at, updated_at
		FROM users
		WHERE oauth_provider = $1 AND oauth_provider_id = $2 AND tenant_id = $3`

	var u user.User
	err := r.db.GetContext(ctx, &u, query, provider, providerID, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, user.ErrUserNotFound().
				WithDetail("oauth_provider", provider).
				WithDetail("oauth_provider_id", providerID)
		}
		return nil, errx.Wrap(err, "failed to find user by oauth provider", errx.TypeInternal).
			WithDetail("oauth_provider", provider).
			WithDetail("oauth_provider_id", providerID).
			WithDetail("tenant_id", tenantID.String())
	}

	return &u, nil
}

package userinfra

import (
	"context"
	"database/sql"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// PostgresUserRepository is the PostgreSQL implementation of UserRepository
type PostgresUserRepository struct {
	db *sqlx.DB
}

// NewPostgresUserRepository creates a new instance of the user repository
func NewPostgresUserRepository(db *sqlx.DB) user.UserRepository {
	return &PostgresUserRepository{
		db: db,
	}
}

// userDB is the database representation with pq.StringArray for scopes
type userDB struct {
	ID              string         `db:"id"`
	TenantID        string         `db:"tenant_id"`
	Email           string         `db:"email"`
	Name            string         `db:"name"`
	Picture         *string        `db:"picture"`
	Status          string         `db:"status"`
	Scopes          pq.StringArray `db:"scopes"`
	OAuthProvider   string         `db:"oauth_provider"`
	OAuthProviderID string         `db:"oauth_provider_id"`
	EmailVerified   bool           `db:"email_verified"`
	OTPEnabled      bool           `db:"otp_enabled"`
	LastLoginAt     sql.NullTime   `db:"last_login_at"` // ✅ NOT a pointer
	CreatedAt       time.Time      `db:"created_at"`    // ✅ Use time.Time directly
	UpdatedAt       time.Time      `db:"updated_at"`    // ✅ Use time.Time directly
}

// toDomain converts database model to domain model
func (db *userDB) toDomain() (*user.User, error) {
	u := &user.User{
		ID:              kernel.UserID(db.ID),
		TenantID:        kernel.TenantID(db.TenantID),
		Email:           db.Email,
		Name:            db.Name,
		Picture:         db.Picture,
		Status:          user.UserStatus(db.Status),
		Scopes:          []string(db.Scopes),
		OAuthProvider:   iam.OAuthProvider(db.OAuthProvider),
		OAuthProviderID: db.OAuthProviderID,
		EmailVerified:   db.EmailVerified,
		OTPEnabled:      db.OTPEnabled,
		CreatedAt:       db.CreatedAt,
		UpdatedAt:       db.UpdatedAt,
	}

	if db.LastLoginAt.Valid {
		u.LastLoginAt = &db.LastLoginAt.Time
	}

	return u, nil
}

// fromDomain converts domain model to database model
func fromDomain(u *user.User) *userDB {
	db := &userDB{
		ID:              u.ID.String(),
		TenantID:        u.TenantID.String(),
		Email:           u.Email,
		Name:            u.Name,
		Picture:         u.Picture,
		Status:          string(u.Status),
		Scopes:          pq.StringArray(u.Scopes),
		OAuthProvider:   string(u.OAuthProvider),
		OAuthProviderID: u.OAuthProviderID,
		EmailVerified:   u.EmailVerified,
		OTPEnabled:      u.OTPEnabled,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}

	if u.LastLoginAt != nil {
		db.LastLoginAt = sql.NullTime{Time: *u.LastLoginAt, Valid: true}
	}

	return db
}

// FindByID finds a user by ID and tenant
func (r *PostgresUserRepository) FindByID(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) (*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified, otp_enabled,
			last_login_at, created_at, updated_at
		FROM users
		WHERE id = $1 AND tenant_id = $2`

	var dbUser userDB
	err := r.db.GetContext(ctx, &dbUser, query, id.String(), tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, user.ErrUserNotFound().WithDetail("user_id", id.String())
		}
		return nil, errx.Wrap(err, "failed to find user by id", errx.TypeInternal).
			WithDetail("user_id", id.String()).
			WithDetail("tenant_id", tenantID.String())
	}

	return dbUser.toDomain()
}

// FindByEmail finds a user by email and tenant
func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified, otp_enabled,
			last_login_at, created_at, updated_at
		FROM users
		WHERE email = $1 AND tenant_id = $2`

	var dbUser userDB
	err := r.db.GetContext(ctx, &dbUser, query, email, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, user.ErrUserNotFound().WithDetail("email", email)
		}
		return nil, errx.Wrap(err, "failed to find user by email", errx.TypeInternal).
			WithDetail("email", email).
			WithDetail("tenant_id", tenantID.String())
	}

	return dbUser.toDomain()
}

// FindByEmailAcrossTenants finds all users with this email across all tenants
func (r *PostgresUserRepository) FindByEmailAcrossTenants(ctx context.Context, email string) ([]*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified, otp_enabled,
			last_login_at, created_at, updated_at
		FROM users
		WHERE email = $1
		ORDER BY created_at DESC`

	var dbUsers []userDB
	err := r.db.SelectContext(ctx, &dbUsers, query, email)
	if err != nil {
		return nil, errx.Wrap(err, "failed to find users by email across tenants", errx.TypeInternal).
			WithDetail("email", email)
	}

	// Convert to domain models
	result := make([]*user.User, len(dbUsers))
	for i := range dbUsers {
		domainUser, err := dbUsers[i].toDomain()
		if err != nil {
			return nil, err
		}
		result[i] = domainUser
	}

	return result, nil
}

// FindByTenant finds all users for a tenant
func (r *PostgresUserRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*user.User, error) {
	query := `
		SELECT
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified, otp_enabled,
			last_login_at, created_at, updated_at
		FROM users
		WHERE tenant_id = $1
		ORDER BY name ASC`

	var dbUsers []userDB
	err := r.db.SelectContext(ctx, &dbUsers, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find users by tenant", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convert to domain models
	result := make([]*user.User, len(dbUsers))
	for i := range dbUsers {
		domainUser, err := dbUsers[i].toDomain()
		if err != nil {
			return nil, err
		}
		result[i] = domainUser
	}

	return result, nil
}

// Save saves or updates a user
func (r *PostgresUserRepository) Save(ctx context.Context, u user.User) error {
	exists, err := r.userExists(ctx, u.ID, u.TenantID)
	if err != nil {
		return errx.Wrap(err, "failed to check user existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, u)
	}
	return r.create(ctx, u)
}

// create creates a new user
func (r *PostgresUserRepository) create(ctx context.Context, u user.User) error {
	query := `
		INSERT INTO users (
			id, tenant_id, email, name, picture, status, scopes,
			oauth_provider, oauth_provider_id, email_verified, otp_enabled,
			last_login_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)`

	_, err := r.db.ExecContext(ctx, query,
		u.ID.String(),
		u.TenantID.String(),
		u.Email,
		u.Name,
		u.Picture,
		u.Status,
		pq.Array(u.Scopes),
		u.OAuthProvider,
		u.OAuthProviderID,
		u.EmailVerified,
		u.OTPEnabled,
		u.LastLoginAt,
		u.CreatedAt,
		u.UpdatedAt,
	)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" && pqErr.Constraint == "uq_users_email_tenant" {
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

// update updates an existing user
func (r *PostgresUserRepository) update(ctx context.Context, u user.User) error {
	query := `
		UPDATE users SET
			email = $1,
			name = $2,
			picture = $3,
			status = $4,
			scopes = $5,
			oauth_provider = $6,
			oauth_provider_id = $7,
			email_verified = $8,
			otp_enabled = $9,
			last_login_at = $10,
			updated_at = $11
		WHERE id = $12 AND tenant_id = $13`

	result, err := r.db.ExecContext(ctx, query,
		u.Email,
		u.Name,
		u.Picture,
		u.Status,
		pq.Array(u.Scopes),
		u.OAuthProvider,
		u.OAuthProviderID,
		u.EmailVerified,
		u.OTPEnabled,
		u.LastLoginAt,
		u.UpdatedAt,
		u.ID.String(),
		u.TenantID.String(),
	)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" && pqErr.Constraint == "uq_users_email_tenant" {
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

// Delete deletes a user
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

// ExistsByEmail checks if a user with the given email exists in the tenant
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

// userExists checks if a user exists by ID and tenant
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



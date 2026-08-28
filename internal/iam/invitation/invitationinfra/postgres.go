package invitationinfra

import (
	"context"
	"database/sql"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// PostgresInvitationRepository is the PostgreSQL implementation of InvitationRepository
type PostgresInvitationRepository struct {
	db *sqlx.DB
}

// NewPostgresInvitationRepository creates a new invitation repository instance
func NewPostgresInvitationRepository(db *sqlx.DB) invitation.InvitationRepository {
	return &PostgresInvitationRepository{
		db: db,
	}
}

// getExecutor returns transaction if present in context, otherwise returns db
func (r *PostgresInvitationRepository) getExecutor(ctx context.Context) sqlx.ExtContext {
	if tx, ok := ctx.Value("db_tx").(*sqlx.Tx); ok {
		return tx
	}
	return r.db
}

// invitationDB is the database representation with pq.StringArray for scopes
type invitationDB struct {
	ID         string         `db:"id"`
	TenantID   string         `db:"tenant_id"`
	Email      string         `db:"email"`
	Token      string         `db:"token"`
	Scopes     pq.StringArray `db:"scopes"`
	RoleID     *string        `db:"role_id"`
	Status     string         `db:"status"`
	InvitedBy  string         `db:"invited_by"`
	ExpiresAt  time.Time      `db:"expires_at"`
	AcceptedAt *time.Time     `db:"accepted_at"`
	AcceptedBy *string        `db:"accepted_by"`
	CreatedAt  time.Time      `db:"created_at"`
	UpdatedAt  time.Time      `db:"updated_at"`
}

// toDomain converts database model to domain model
func (db *invitationDB) toDomain() (*invitation.Invitation, error) {
	inv := &invitation.Invitation{
		ID:         kernel.NewInvitationID(db.ID),
		TenantID:   kernel.TenantID(db.TenantID),
		Email:      db.Email,
		Token:      db.Token,
		Scopes:     []string(db.Scopes),
		Status:     invitation.InvitationStatus(db.Status),
		InvitedBy:  kernel.UserID(db.InvitedBy),
		ExpiresAt:  db.ExpiresAt,
		AcceptedAt: db.AcceptedAt,
		CreatedAt:  db.CreatedAt,
		UpdatedAt:  db.UpdatedAt,
	}

	if db.RoleID != nil {
		roleID := kernel.NewRoleID(*db.RoleID)
		inv.RoleID = &roleID
	}

	if db.AcceptedBy != nil {
		acceptedBy := kernel.UserID(*db.AcceptedBy)
		inv.AcceptedBy = &acceptedBy
	}

	return inv, nil
}

// FindByID finds an invitation by ID
func (r *PostgresInvitationRepository) FindByID(ctx context.Context, id kernel.InvitationID) (*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE id = $1`

	var dbInv invitationDB
	err := sqlx.GetContext(ctx, executor, &dbInv, query, id.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, invitation.ErrInvitationNotFound().WithDetail("invitation_id", id.String())
		}
		return nil, errx.Wrap(err, "failed to find invitation by id", errx.TypeInternal).
			WithDetail("invitation_id", id.String())
	}

	return dbInv.toDomain()
}

// FindByToken finds an invitation by token
func (r *PostgresInvitationRepository) FindByToken(ctx context.Context, token string) (*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE token = $1`

	var dbInv invitationDB
	err := sqlx.GetContext(ctx, executor, &dbInv, query, token)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, invitation.ErrInvitationNotFound().WithDetail("token", token)
		}
		return nil, errx.Wrap(err, "failed to find invitation by token", errx.TypeInternal)
	}

	return dbInv.toDomain()
}

// FindByEmail finds invitations by email
func (r *PostgresInvitationRepository) FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) ([]*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE email = $1 AND tenant_id = $2
		ORDER BY created_at DESC`

	var dbInvitations []invitationDB
	err := sqlx.SelectContext(ctx, executor, &dbInvitations, query, email, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find invitations by email", errx.TypeInternal).
			WithDetail("email", email)
	}

	// Convert to slice of pointers
	result := make([]*invitation.Invitation, len(dbInvitations))
	for i := range dbInvitations {
		domainInv, err := dbInvitations[i].toDomain()
		if err != nil {
			return nil, err
		}
		result[i] = domainInv
	}

	return result, nil
}

// FindPendingByEmail finds pending invitations for an email in a tenant
func (r *PostgresInvitationRepository) FindPendingByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE email = $1 AND tenant_id = $2 AND status = 'PENDING' AND expires_at > NOW()
		ORDER BY created_at DESC
		LIMIT 1`

	var dbInv invitationDB
	err := sqlx.GetContext(ctx, executor, &dbInv, query, email, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, invitation.ErrInvitationNotFound().WithDetail("email", email)
		}
		return nil, errx.Wrap(err, "failed to find pending invitation", errx.TypeInternal).
			WithDetail("email", email)
	}

	return dbInv.toDomain()
}

// FindByTenant finds all invitations for a tenant
func (r *PostgresInvitationRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE tenant_id = $1
		ORDER BY created_at DESC`

	var dbInvitations []invitationDB
	err := sqlx.SelectContext(ctx, executor, &dbInvitations, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find invitations by tenant", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convert to slice of pointers
	result := make([]*invitation.Invitation, len(dbInvitations))
	for i := range dbInvitations {
		domainInv, err := dbInvitations[i].toDomain()
		if err != nil {
			return nil, err
		}
		result[i] = domainInv
	}

	return result, nil
}

// FindPendingByTenant finds pending invitations for a tenant
func (r *PostgresInvitationRepository) FindPendingByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE tenant_id = $1 AND status = 'PENDING' AND expires_at > NOW()
		ORDER BY created_at DESC`

	var dbInvitations []invitationDB
	err := sqlx.SelectContext(ctx, executor, &dbInvitations, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find pending invitations", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convert to slice of pointers
	result := make([]*invitation.Invitation, len(dbInvitations))
	for i := range dbInvitations {
		domainInv, err := dbInvitations[i].toDomain()
		if err != nil {
			return nil, err
		}
		result[i] = domainInv
	}

	return result, nil
}

// FindExpired finds expired invitations
func (r *PostgresInvitationRepository) FindExpired(ctx context.Context) ([]*invitation.Invitation, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM invitations
		WHERE status = 'PENDING' AND expires_at < NOW()`

	var dbInvitations []invitationDB
	err := sqlx.SelectContext(ctx, executor, &dbInvitations, query)
	if err != nil {
		return nil, errx.Wrap(err, "failed to find expired invitations", errx.TypeInternal)
	}

	// Convert to slice of pointers
	result := make([]*invitation.Invitation, len(dbInvitations))
	for i := range dbInvitations {
		domainInv, err := dbInvitations[i].toDomain()
		if err != nil {
			return nil, err
		}
		result[i] = domainInv
	}

	return result, nil
}

// Save saves or updates an invitation
func (r *PostgresInvitationRepository) Save(ctx context.Context, inv invitation.Invitation) error {
	// Check if the invitation already exists
	exists, err := r.invitationExists(ctx, inv.ID.String())
	if err != nil {
		return errx.Wrap(err, "failed to check invitation existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, inv)
	}
	return r.create(ctx, inv)
}

// create creates a new invitation
func (r *PostgresInvitationRepository) create(ctx context.Context, inv invitation.Invitation) error {
	executor := r.getExecutor(ctx)

	query := `
		INSERT INTO invitations (
			id, tenant_id, email, token, scopes, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)`

	_, err := executor.ExecContext(ctx, query,
		inv.ID.String(),
		inv.TenantID,
		inv.Email,
		inv.Token,
		pq.Array(inv.Scopes),
		fromRoleID(inv.RoleID),
		inv.Status,
		inv.InvitedBy,
		inv.ExpiresAt,
		inv.AcceptedAt,
		inv.AcceptedBy,
		inv.CreatedAt,
		inv.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" {
				return invitation.ErrInvitationAlreadyExists().
					WithDetail("email", inv.Email)
			}
		}
		return errx.Wrap(err, "failed to create invitation", errx.TypeInternal).
			WithDetail("invitation_id", inv.ID.String())
	}

	return nil
}

// update updates an existing invitation
func (r *PostgresInvitationRepository) update(ctx context.Context, inv invitation.Invitation) error {
	executor := r.getExecutor(ctx)

	query := `
		UPDATE invitations SET
			email = $1,
			status = $2,
			scopes = $3,
			role_id = $4,
			expires_at = $5,
			accepted_at = $6,
			accepted_by = $7,
			updated_at = $8
		WHERE id = $9`

	result, err := executor.ExecContext(ctx, query,
		inv.Email,
		inv.Status,
		pq.Array(inv.Scopes),
		fromRoleID(inv.RoleID),
		inv.ExpiresAt,
		inv.AcceptedAt,
		inv.AcceptedBy,
		inv.UpdatedAt,
		inv.ID.String(),
	)

	if err != nil {
		return errx.Wrap(err, "failed to update invitation", errx.TypeInternal).
			WithDetail("invitation_id", inv.ID.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return invitation.ErrInvitationNotFound().WithDetail("invitation_id", inv.ID.String())
	}

	return nil
}

// Delete deletes an invitation
func (r *PostgresInvitationRepository) Delete(ctx context.Context, id kernel.InvitationID) error {
	executor := r.getExecutor(ctx)

	query := `DELETE FROM invitations WHERE id = $1`

	result, err := executor.ExecContext(ctx, query, id.String())
	if err != nil {
		return errx.Wrap(err, "failed to delete invitation", errx.TypeInternal).
			WithDetail("invitation_id", id.String())
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return invitation.ErrInvitationNotFound().WithDetail("invitation_id", id.String())
	}

	return nil
}

// ExistsPendingForEmail checks if a pending invitation exists for an email
func (r *PostgresInvitationRepository) ExistsPendingForEmail(ctx context.Context, email string, tenantID kernel.TenantID) (bool, error) {
	executor := r.getExecutor(ctx)

	query := `
		SELECT EXISTS(
			SELECT 1 FROM invitations
			WHERE email = $1 AND tenant_id = $2 AND status = 'PENDING' AND expires_at > NOW()
		)`

	var exists bool
	err := sqlx.GetContext(ctx, executor, &exists, query, email, tenantID.String())
	if err != nil {
		return false, errx.Wrap(err, "failed to check pending invitation existence", errx.TypeInternal).
			WithDetail("email", email)
	}

	return exists, nil
}

// fromRoleID converts a *kernel.RoleID to a *string for DB storage
func fromRoleID(roleID *kernel.RoleID) *string {
	if roleID == nil {
		return nil
	}
	s := roleID.String()
	return &s
}

// invitationExists checks if an invitation exists by ID
func (r *PostgresInvitationRepository) invitationExists(ctx context.Context, id string) (bool, error) {
	executor := r.getExecutor(ctx)

	query := `SELECT EXISTS(SELECT 1 FROM invitations WHERE id = $1)`

	var exists bool
	err := sqlx.GetContext(ctx, executor, &exists, query, id)
	if err != nil {
		return false, errx.Wrap(err, "failed to check invitation existence", errx.TypeInternal).
			WithDetail("invitation_id", id)
	}

	return exists, nil
}

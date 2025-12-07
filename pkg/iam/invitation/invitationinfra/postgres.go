package invitationinfra

import (
	"context"
	"database/sql"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/invitation"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// PostgresInvitationRepository implementación de PostgreSQL para InvitationRepository
type PostgresInvitationRepository struct {
	db *sqlx.DB
}

// NewPostgresInvitationRepository crea una nueva instancia del repositorio de invitaciones
func NewPostgresInvitationRepository(db *sqlx.DB) invitation.InvitationRepository {
	return &PostgresInvitationRepository{
		db: db,
	}
}

// FindByID busca una invitación por ID
func (r *PostgresInvitationRepository) FindByID(ctx context.Context, id string) (*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE id = $1`

	var inv invitation.Invitation
	err := r.db.GetContext(ctx, &inv, query, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, invitation.ErrInvitationNotFound().WithDetail("invitation_id", id)
		}
		return nil, errx.Wrap(err, "failed to find invitation by id", errx.TypeInternal).
			WithDetail("invitation_id", id)
	}

	return &inv, nil
}

// FindByToken busca una invitación por token
func (r *PostgresInvitationRepository) FindByToken(ctx context.Context, token string) (*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE token = $1`

	var inv invitation.Invitation
	err := r.db.GetContext(ctx, &inv, query, token)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, invitation.ErrInvitationNotFound().WithDetail("token", token)
		}
		return nil, errx.Wrap(err, "failed to find invitation by token", errx.TypeInternal)
	}

	return &inv, nil
}

// FindByEmail busca invitaciones por email
func (r *PostgresInvitationRepository) FindByEmail(ctx context.Context, email string, tenantID kernel.TenantID) ([]*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE email = $1 AND tenant_id = $2
		ORDER BY created_at DESC`

	var invitations []invitation.Invitation
	err := r.db.SelectContext(ctx, &invitations, query, email, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find invitations by email", errx.TypeInternal).
			WithDetail("email", email)
	}

	// Convertir a slice de punteros
	result := make([]*invitation.Invitation, len(invitations))
	for i := range invitations {
		result[i] = &invitations[i]
	}

	return result, nil
}

// FindPendingByEmail busca invitaciones pendientes para un email en un tenant
func (r *PostgresInvitationRepository) FindPendingByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE email = $1 AND tenant_id = $2 AND status = 'PENDING' AND expires_at > NOW()
		ORDER BY created_at DESC
		LIMIT 1`

	var inv invitation.Invitation
	err := r.db.GetContext(ctx, &inv, query, email, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, invitation.ErrInvitationNotFound().WithDetail("email", email)
		}
		return nil, errx.Wrap(err, "failed to find pending invitation", errx.TypeInternal).
			WithDetail("email", email)
	}

	return &inv, nil
}

// FindByTenant busca todas las invitaciones de un tenant
func (r *PostgresInvitationRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE tenant_id = $1
		ORDER BY created_at DESC`

	var invitations []invitation.Invitation
	err := r.db.SelectContext(ctx, &invitations, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find invitations by tenant", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convertir a slice de punteros
	result := make([]*invitation.Invitation, len(invitations))
	for i := range invitations {
		result[i] = &invitations[i]
	}

	return result, nil
}

// FindPendingByTenant busca invitaciones pendientes de un tenant
func (r *PostgresInvitationRepository) FindPendingByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE tenant_id = $1 AND status = 'PENDING' AND expires_at > NOW()
		ORDER BY created_at DESC`

	var invitations []invitation.Invitation
	err := r.db.SelectContext(ctx, &invitations, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find pending invitations", errx.TypeInternal).
			WithDetail("tenant_id", tenantID.String())
	}

	// Convertir a slice de punteros
	result := make([]*invitation.Invitation, len(invitations))
	for i := range invitations {
		result[i] = &invitations[i]
	}

	return result, nil
}

// FindExpired busca invitaciones expiradas
func (r *PostgresInvitationRepository) FindExpired(ctx context.Context) ([]*invitation.Invitation, error) {
	query := `
		SELECT
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		FROM user_invitations
		WHERE status = 'PENDING' AND expires_at < NOW()`

	var invitations []invitation.Invitation
	err := r.db.SelectContext(ctx, &invitations, query)
	if err != nil {
		return nil, errx.Wrap(err, "failed to find expired invitations", errx.TypeInternal)
	}

	// Convertir a slice de punteros
	result := make([]*invitation.Invitation, len(invitations))
	for i := range invitations {
		result[i] = &invitations[i]
	}

	return result, nil
}

// Save guarda o actualiza una invitación
func (r *PostgresInvitationRepository) Save(ctx context.Context, inv invitation.Invitation) error {
	// Verificar si la invitación ya existe
	exists, err := r.invitationExists(ctx, inv.ID)
	if err != nil {
		return errx.Wrap(err, "failed to check invitation existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, inv)
	}
	return r.create(ctx, inv)
}

// create crea una nueva invitación
func (r *PostgresInvitationRepository) create(ctx context.Context, inv invitation.Invitation) error {
	query := `
		INSERT INTO user_invitations (
			id, tenant_id, email, token, role_id, status, invited_by,
			expires_at, accepted_at, accepted_by, created_at, updated_at
		) VALUES (
			:id, :tenant_id, :email, :token, :role_id, :status, :invited_by,
			:expires_at, :accepted_at, :accepted_by, :created_at, :updated_at
		)`

	_, err := r.db.NamedExecContext(ctx, query, inv)
	if err != nil {
		// Verificar violación de constraint único
		if pqErr, ok := err.(*pq.Error); ok {
			if pqErr.Code == "23505" {
				return invitation.ErrInvitationAlreadyExists().
					WithDetail("email", inv.Email)
			}
		}
		return errx.Wrap(err, "failed to create invitation", errx.TypeInternal).
			WithDetail("invitation_id", inv.ID)
	}

	return nil
}

// update actualiza una invitación existente
func (r *PostgresInvitationRepository) update(ctx context.Context, inv invitation.Invitation) error {
	query := `
		UPDATE user_invitations SET
			email = :email,
			status = :status,
			role_id = :role_id,
			expires_at = :expires_at,
			accepted_at = :accepted_at,
			accepted_by = :accepted_by,
			updated_at = :updated_at
		WHERE id = :id`

	result, err := r.db.NamedExecContext(ctx, query, inv)
	if err != nil {
		return errx.Wrap(err, "failed to update invitation", errx.TypeInternal).
			WithDetail("invitation_id", inv.ID)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return invitation.ErrInvitationNotFound().WithDetail("invitation_id", inv.ID)
	}

	return nil
}

// Delete elimina una invitación
func (r *PostgresInvitationRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM user_invitations WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errx.Wrap(err, "failed to delete invitation", errx.TypeInternal).
			WithDetail("invitation_id", id)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errx.Wrap(err, "failed to get rows affected", errx.TypeInternal)
	}

	if rowsAffected == 0 {
		return invitation.ErrInvitationNotFound().WithDetail("invitation_id", id)
	}

	return nil
}

// ExistsPendingForEmail verifica si existe una invitación pendiente para un email
func (r *PostgresInvitationRepository) ExistsPendingForEmail(ctx context.Context, email string, tenantID kernel.TenantID) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM user_invitations
			WHERE email = $1 AND tenant_id = $2 AND status = 'PENDING' AND expires_at > NOW()
		)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, email, tenantID.String())
	if err != nil {
		return false, errx.Wrap(err, "failed to check pending invitation existence", errx.TypeInternal).
			WithDetail("email", email)
	}

	return exists, nil
}

// invitationExists verifica si una invitación existe por ID
func (r *PostgresInvitationRepository) invitationExists(ctx context.Context, id string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM user_invitations WHERE id = $1)`

	var exists bool
	err := r.db.GetContext(ctx, &exists, query, id)
	if err != nil {
		return false, errx.Wrap(err, "failed to check invitation existence", errx.TypeInternal).
			WithDetail("invitation_id", id)
	}

	return exists, nil
}

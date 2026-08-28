package roleinfra

import (
	"context"
	"database/sql"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type PostgresRoleRepository struct {
	db *sqlx.DB
}

func NewPostgresRoleRepository(db *sqlx.DB) role.RoleRepository {
	return &PostgresRoleRepository{db: db}
}

func (r *PostgresRoleRepository) Save(ctx context.Context, rl role.Role) error {
	exists, err := r.roleExists(ctx, rl.ID.String())
	if err != nil {
		return errx.Wrap(err, "failed to check role existence", errx.TypeInternal)
	}

	if exists {
		return r.update(ctx, rl)
	}
	return r.create(ctx, rl)
}

func (r *PostgresRoleRepository) create(ctx context.Context, rl role.Role) error {
	query := `
		INSERT INTO roles (id, tenant_id, name, description, scopes, created_at, updated_at)
		VALUES (:id, :tenant_id, :name, :description, :scopes, :created_at, :updated_at)`

	p := toPersistence(rl)
	_, err := r.db.NamedExecContext(ctx, query, p)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return role.ErrRoleAlreadyExists()
		}
		return errx.Wrap(err, "failed to create role", errx.TypeInternal)
	}
	return nil
}

func (r *PostgresRoleRepository) update(ctx context.Context, rl role.Role) error {
	query := `
		UPDATE roles SET
			name = :name,
			description = :description,
			scopes = :scopes,
			updated_at = :updated_at
		WHERE id = :id AND tenant_id = :tenant_id`

	p := toPersistence(rl)
	result, err := r.db.NamedExecContext(ctx, query, p)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return role.ErrRoleAlreadyExists()
		}
		return errx.Wrap(err, "failed to update role", errx.TypeInternal)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return role.ErrRoleNotFound()
	}
	return nil
}

func (r *PostgresRoleRepository) FindByID(ctx context.Context, id kernel.RoleID, tenantID kernel.TenantID) (*role.Role, error) {
	var p rolePersistence
	query := `SELECT * FROM roles WHERE id = $1 AND tenant_id = $2`
	err := r.db.GetContext(ctx, &p, query, id.String(), tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, role.ErrRoleNotFound()
		}
		return nil, errx.Wrap(err, "failed to find role by ID", errx.TypeInternal)
	}
	d := toDomain(p)
	return &d, nil
}

func (r *PostgresRoleRepository) FindByName(ctx context.Context, name string, tenantID kernel.TenantID) (*role.Role, error) {
	var p rolePersistence
	query := `SELECT * FROM roles WHERE name = $1 AND tenant_id = $2`
	err := r.db.GetContext(ctx, &p, query, name, tenantID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, role.ErrRoleNotFound()
		}
		return nil, errx.Wrap(err, "failed to find role by name", errx.TypeInternal)
	}
	d := toDomain(p)
	return &d, nil
}

func (r *PostgresRoleRepository) FindByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*role.Role, error) {
	var roles []rolePersistence
	query := `SELECT * FROM roles WHERE tenant_id = $1 ORDER BY name`
	err := r.db.SelectContext(ctx, &roles, query, tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find roles by tenant", errx.TypeInternal)
	}
	return toDomainSlice(roles), nil
}

func (r *PostgresRoleRepository) Delete(ctx context.Context, id kernel.RoleID, tenantID kernel.TenantID) error {
	// Delete role assignments first, then the role
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return errx.Wrap(err, "failed to begin transaction", errx.TypeInternal)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM user_roles WHERE role_id = $1 AND tenant_id = $2`, id.String(), tenantID.String())
	if err != nil {
		return errx.Wrap(err, "failed to delete role assignments", errx.TypeInternal)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id = $1 AND tenant_id = $2`, id.String(), tenantID.String())
	if err != nil {
		return errx.Wrap(err, "failed to delete role", errx.TypeInternal)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return role.ErrRoleNotFound()
	}

	return tx.Commit()
}

func (r *PostgresRoleRepository) AssignToUser(ctx context.Context, userRole role.UserRole) error {
	query := `
		INSERT INTO user_roles (user_id, role_id, tenant_id, assigned_at)
		VALUES ($1, $2, $3, $4)`

	_, err := r.db.ExecContext(ctx, query,
		userRole.UserID.String(), userRole.RoleID.String(), userRole.TenantID.String(), userRole.AssignedAt)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return role.ErrRoleAlreadyAssigned()
		}
		return errx.Wrap(err, "failed to assign role to user", errx.TypeInternal)
	}
	return nil
}

func (r *PostgresRoleRepository) UnassignFromUser(ctx context.Context, userID kernel.UserID, roleID kernel.RoleID, tenantID kernel.TenantID) error {
	query := `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2 AND tenant_id = $3`
	result, err := r.db.ExecContext(ctx, query, userID.String(), roleID.String(), tenantID.String())
	if err != nil {
		return errx.Wrap(err, "failed to unassign role from user", errx.TypeInternal)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return role.ErrRoleNotAssigned()
	}
	return nil
}

func (r *PostgresRoleRepository) FindByUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) ([]*role.Role, error) {
	var roles []rolePersistence
	query := `
		SELECT r.* FROM roles r
		INNER JOIN user_roles ur ON r.id = ur.role_id AND r.tenant_id = ur.tenant_id
		WHERE ur.user_id = $1 AND ur.tenant_id = $2
		ORDER BY r.name`
	err := r.db.SelectContext(ctx, &roles, query, userID.String(), tenantID.String())
	if err != nil {
		return nil, errx.Wrap(err, "failed to find roles by user", errx.TypeInternal)
	}
	return toDomainSlice(roles), nil
}

func (r *PostgresRoleRepository) roleExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM roles WHERE id = $1)`
	err := r.db.GetContext(ctx, &exists, query, id)
	return exists, err
}

// Persistence model
type rolePersistence struct {
	ID          string          `db:"id"`
	TenantID    kernel.TenantID `db:"tenant_id"`
	Name        string          `db:"name"`
	Description sql.NullString  `db:"description"`
	Scopes      pq.StringArray  `db:"scopes"`
	CreatedAt   time.Time       `db:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

func toPersistence(rl role.Role) rolePersistence {
	return rolePersistence{
		ID:          rl.ID.String(),
		TenantID:    rl.TenantID,
		Name:        rl.Name,
		Description: sql.NullString{String: rl.Description, Valid: rl.Description != ""},
		Scopes:      rl.Scopes,
		CreatedAt:   rl.CreatedAt,
		UpdatedAt:   rl.UpdatedAt,
	}
}

func toDomain(p rolePersistence) role.Role {
	return role.Role{
		ID:          kernel.NewRoleID(p.ID),
		TenantID:    p.TenantID,
		Name:        p.Name,
		Description: p.Description.String,
		Scopes:      p.Scopes,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func toDomainSlice(ps []rolePersistence) []*role.Role {
	result := make([]*role.Role, len(ps))
	for i, p := range ps {
		d := toDomain(p)
		result[i] = &d
	}
	return result
}

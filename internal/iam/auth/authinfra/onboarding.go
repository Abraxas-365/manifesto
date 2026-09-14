package authinfra

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation/invitationinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantinfra"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/iam/user/userinfra"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type PostgresInvitationAcceptor struct{ db *sqlx.DB }

func NewPostgresInvitationAcceptor(db *sqlx.DB) auth.InvitationAcceptor {
	return &PostgresInvitationAcceptor{db: db}
}

func (a *PostgresInvitationAcceptor) Accept(ctx context.Context, token string, candidate user.User) (_ *user.User, _ *tenant.Tenant, retErr error) {
	if !candidate.EmailVerified || candidate.Email == "" {
		return nil, nil, user.ErrEmailNotVerified()
	}
	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			retErr = errors.Join(retErr, err)
		}
	}()

	// Serialize acceptance against revocation/deletion and concurrent redemption.
	var id string
	if err := tx.GetContext(ctx, &id, `SELECT id FROM invitations WHERE token = $1 FOR UPDATE`, token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, invitation.ErrInvitationNotFound()
		}
		return nil, nil, err
	}
	invitations := invitationinfra.NewPostgresInvitationRepository(tx)
	inv, err := invitations.FindByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	if !inv.CanBeAccepted() || inv.Email != candidate.Email || (!candidate.TenantID.IsEmpty() && candidate.TenantID != inv.TenantID) {
		return nil, nil, invitation.ErrInvitationInvalid()
	}
	for _, scope := range inv.Scopes {
		if !scopes.ValidateScope(scope) {
			return nil, nil, invitation.ErrInvalidScopes()
		}
	}
	// Lock tenant capacity and status until membership has committed.
	if err := tx.GetContext(ctx, &id, `SELECT id FROM tenants WHERE id = $1 FOR UPDATE`, inv.TenantID.String()); err != nil {
		return nil, nil, err
	}
	tenants := tenantinfra.NewPostgresTenantRepository(tx)
	t, err := tenants.FindByID(ctx, inv.TenantID)
	if err != nil {
		return nil, nil, err
	}
	if !t.IsActive() {
		return nil, nil, tenant.ErrTenantSuspended()
	}

	users := userinfra.NewPostgresUserRepository(tx)
	// Lock existing membership before applying grants or linking authentication.
	var ids []string
	if err := tx.SelectContext(ctx, &ids, `SELECT id FROM users WHERE email = $1 AND tenant_id = $2 FOR UPDATE`, inv.Email, inv.TenantID.String()); err != nil {
		return nil, nil, err
	}
	u, err := users.FindByEmail(ctx, inv.Email, inv.TenantID)
	if err != nil && !errors.Is(err, user.ErrUserNotFound()) {
		return nil, nil, err
	}
	if u == nil {
		if err := t.AddUser(); err != nil {
			return nil, nil, err
		}
		u = &user.User{ID: kernel.NewUserID(uuid.NewString()), TenantID: inv.TenantID,
			Email: inv.Email, Name: candidate.Name, Status: user.UserStatusPending,
			Scopes: []string{}, CreatedAt: time.Now().UTC()}
		if err := tenants.Save(ctx, *t); err != nil {
			return nil, nil, err
		}
	} else if u.Status != user.UserStatusPending && !u.IsActive() {
		return nil, nil, user.ErrInvalidStatus()
	}
	if candidate.HasOAuth() {
		// An invitation cannot replace an already linked provider identity.
		if u.HasOAuth() && (u.OAuthProvider != candidate.OAuthProvider || u.OAuthProviderID != candidate.OAuthProviderID) {
			return nil, nil, user.ErrInvalidStatus()
		}
		u.LinkOAuth(candidate.OAuthProvider, candidate.OAuthProviderID)
		u.Picture = candidate.Picture
	}
	if candidate.OTPEnabled {
		u.EnableOTP()
	}
	if !u.HasOTP() && !u.HasOAuth() {
		return nil, nil, user.ErrInvalidStatus()
	}
	for _, scope := range inv.Scopes {
		u.AddScope(scope)
	}
	u.EmailVerified = true
	if u.Status == user.UserStatusPending {
		if err := u.Activate(); err != nil {
			return nil, nil, err
		}
	}
	u.UpdatedAt = time.Now().UTC()
	if err := users.Save(ctx, *u); err != nil {
		return nil, nil, err
	}
	if inv.RoleID != nil && !inv.RoleID.IsEmpty() {
		var roleScopes pq.StringArray
		if err := tx.GetContext(ctx, &roleScopes, `SELECT scopes FROM roles WHERE id = $1 AND tenant_id = $2 FOR SHARE`, inv.RoleID.String(), inv.TenantID.String()); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil, role.ErrRoleNotFound()
			}
			return nil, nil, err
		}
		for _, scope := range roleScopes {
			if !scopes.ValidateScope(scope) {
				return nil, nil, role.ErrRoleInvalidScopes()
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_roles (user_id, role_id, tenant_id, assigned_at) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, u.ID.String(), inv.RoleID.String(), inv.TenantID.String(), time.Now().UTC()); err != nil {
			return nil, nil, err
		}
	}
	if err := inv.Accept(u.ID); err != nil {
		return nil, nil, err
	}
	if err := invitations.Save(ctx, *inv); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return u, t, nil
}

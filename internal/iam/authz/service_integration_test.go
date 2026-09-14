package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey/apikeysrv"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation/invitationsrv"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/iam/role/rolesrv"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant/tenantsrv"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/iam/user/usersrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

type usersStub struct {
	user.UserRepository
	writes int
	reads  int
	entity user.User
}

func (r *usersStub) FindByID(context.Context, kernel.UserID, kernel.TenantID) (*user.User, error) {
	r.reads++
	u := r.entity
	return &u, nil
}
func (r *usersStub) FindByEmail(context.Context, string, kernel.TenantID) (*user.User, error) {
	return nil, user.ErrUserNotFound()
}
func (r *usersStub) Save(_ context.Context, u user.User) error { r.writes++; r.entity = u; return nil }

type rolesStub struct {
	role.RoleRepository
	writes int
	reads  int
	scopes []string
}

func (r *rolesStub) FindByID(context.Context, kernel.RoleID, kernel.TenantID) (*role.Role, error) {
	r.reads++
	return &role.Role{ID: "role", TenantID: "tenant", Scopes: r.scopes}, nil
}
func (r *rolesStub) AssignToUser(context.Context, role.UserRole) error { r.writes++; return nil }
func (r *rolesStub) Save(context.Context, role.Role) error             { r.writes++; return nil }

type tenantsStub struct {
	tenant.TenantRepository
	reads int
}

func (r *tenantsStub) FindByID(context.Context, kernel.TenantID) (*tenant.Tenant, error) {
	r.reads++
	return &tenant.Tenant{ID: "tenant", Status: tenant.TenantStatusActive}, nil
}

type keysStub struct {
	apikey.APIKeyRepository
	writes int
}

func (r *keysStub) Save(context.Context, apikey.APIKey) error { r.writes++; return nil }

type invitationsStub struct {
	invitation.InvitationRepository
	writes int
}

func (r *invitationsStub) ExistsPendingForEmail(context.Context, string, kernel.TenantID) (bool, error) {
	return false, nil
}
func (r *invitationsStub) Save(context.Context, invitation.Invitation) error { r.writes++; return nil }

func TestPublicServiceGrantEnforcement(t *testing.T) {
	ctx := context.Background()
	for _, covered := range []bool{false, true} {
		name := "uncovered"
		if covered {
			name = "covered"
		}
		t.Run(name, func(t *testing.T) {
			authority := func(operation string) *kernel.AuthContext {
				grants := []string{operation}
				if covered {
					grants = append(grants, "users:*")
				}
				return &kernel.AuthContext{Actor: kernel.NewUserActor("creator"), TenantID: "tenant", Scopes: grants}
			}
			expect := func(err error, writes int) {
				t.Helper()
				if covered {
					if err != nil || writes != 1 {
						t.Fatalf("covered grant failed: %v, writes=%d", err, writes)
					}
				} else if err == nil || writes != 0 {
					t.Fatalf("uncovered grant persisted: %v, writes=%d", err, writes)
				}
			}
			t.Run("user replacement", func(t *testing.T) {
				r := &usersStub{}
				s := usersrv.NewUserService(r, nil, nil)
				err := s.SetUserScopes(ctx, authority("scopes:write"), "member", "tenant", []string{"users:read"})
				expect(err, r.writes)
			})
			t.Run("profile replacement", func(t *testing.T) {
				r := &usersStub{}
				s := usersrv.NewUserService(r, nil, nil)
				ac := authority("users:write")
				ac.Scopes = append(ac.Scopes, "scopes:write")
				_, err := s.UpdateUser(ctx, ac, "member", user.UpdateUserRequest{TenantID: "tenant", Scopes: []string{"users:read"}})
				expect(err, r.writes)
			})
			t.Run("role assignment", func(t *testing.T) {
				r := &rolesStub{scopes: []string{"users:read"}}
				s := rolesrv.NewRoleService(r, nil, &usersStub{})
				err := s.AssignRoleToUser(ctx, authority("roles:assign"), "role", "member", "tenant")
				expect(err, r.writes)
			})
			t.Run("role replacement", func(t *testing.T) {
				r := &rolesStub{}
				s := rolesrv.NewRoleService(r, nil, nil)
				_, err := s.UpdateRole(ctx, authority("roles:write"), "role", "tenant", role.UpdateRoleRequest{Scopes: []string{"users:read"}})
				expect(err, r.writes)
			})
			t.Run("key creation", func(t *testing.T) {
				r := &keysStub{}
				s := apikeysrv.NewAPIKeyService(r, &tenantsStub{}, &usersStub{})
				_, err := s.CreateAPIKey(ctx, authority("api_keys:write"), "tenant", "creator", apikey.CreateAPIKeyRequest{Name: "service-key", Scopes: []string{"users:read"}, Environment: "test"})
				expect(err, r.writes)
			})
			for _, viaRole := range []bool{false, true} {
				label := "direct invitation"
				if viaRole {
					label = "role invitation"
				}
				t.Run(label, func(t *testing.T) {
					r := &invitationsStub{}
					s := invitationsrv.NewInvitationService(r, &usersStub{}, &tenantsStub{}, &rolesStub{scopes: []string{"users:read"}}, nil, &config.InvitationConfig{TokenByteLength: 32, DefaultExpirationDays: 7})
					req := invitation.CreateInvitationRequest{Email: "join@example.com", Scopes: []string{"users:read"}}
					if viaRole {
						id := kernel.NewRoleID("role")
						req.RoleID = &id
						req.Scopes = nil
					}
					_, err := s.CreateInvitation(ctx, authority("invitations:write"), "tenant", "creator", req)
					expect(err, r.writes)
				})
			}
		})
	}
}

func TestExactServiceScopeAndRetiredGrantRemoval(t *testing.T) {
	ctx := context.Background()
	r := &usersStub{entity: user.User{Scopes: []string{"platform:old"}}}
	s := usersrv.NewUserService(r, nil, nil)
	ac := &kernel.AuthContext{Actor: kernel.NewAPIKeyActor("key"), TenantID: "tenant", Scopes: []string{"users:read"}}
	if _, err := s.GetUserByID(ctx, ac, "member", "tenant"); err != nil || r.reads != 1 {
		t.Fatal("exact read permission rejected", err)
	}
	ac.Scopes = []string{"users:write"}
	if _, err := s.GetUserByID(ctx, ac, "member", "tenant"); !errors.Is(err, iam.ErrAccessDenied()) || r.reads != 1 {
		t.Fatal("unrelated permission reached repository", err)
	}
	ac.Scopes = []string{"scopes:assign"}
	if err := s.RemoveScopesFromUser(ctx, ac, "member", "tenant", []string{"platform:old"}); err != nil || r.writes != 1 || len(r.entity.Scopes) != 0 {
		t.Fatal("retired grant removal rejected", err)
	}
	tr := &tenantsStub{}
	ts := tenantsrv.NewTenantService(tr, nil, nil, nil)
	ac.Scopes = []string{"tenants:read"}
	if _, err := ts.GetTenantUsage(ctx, ac, "tenant"); err != nil || tr.reads != 1 {
		t.Fatal("exact tenant read permission rejected", err)
	}
}

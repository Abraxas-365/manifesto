package rolesrv

import (
	"context"
	"errors"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"testing"
)

func TestServiceAuthorizationBeforeRepositories(t *testing.T) {
	service := &RoleService{}
	for _, operation := range []struct {
		name string
		call func(*kernel.AuthContext, kernel.TenantID) error
	}{
		{"CreateRole", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.CreateRole(context.Background(), ac, target, role.CreateRoleRequest{})
			return err
		}},
		{"GetRoleByID", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetRoleByID(context.Background(), ac, kernel.RoleID("id"), target)
			return err
		}},
		{"GetTenantRoles", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantRoles(context.Background(), ac, target)
			return err
		}},
		{"UpdateRole", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.UpdateRole(context.Background(), ac, kernel.RoleID("id"), target, role.UpdateRoleRequest{})
			return err
		}},
		{"DeleteRole", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.DeleteRole(context.Background(), ac, kernel.RoleID("id"), target)
			return err
		}},
		{"AssignRoleToUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.AssignRoleToUser(context.Background(), ac, kernel.RoleID("id"), kernel.UserID("id"), target)
			return err
		}},
		{"UnassignRoleFromUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.UnassignRoleFromUser(context.Background(), ac, kernel.RoleID("id"), kernel.UserID("id"), target)
			return err
		}},
		{"GetUserRoles", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetUserRoles(context.Background(), ac, kernel.UserID("id"), target)
			return err
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			target := kernel.NewTenantID("tenant")
			for _, tc := range []struct {
				name string
				ac   *kernel.AuthContext
				want error
			}{
				{"nil", nil, iam.ErrUnauthorized()},
				{"invalid", &kernel.AuthContext{TenantID: target, Scopes: []string{"*"}}, iam.ErrUnauthorized()},
				{"missing scope", &kernel.AuthContext{Actor: kernel.NewUserActor("user"), TenantID: target}, iam.ErrAccessDenied()},
				{"cross tenant wildcard", &kernel.AuthContext{Actor: kernel.NewAPIKeyActor("key"), TenantID: "other", Scopes: []string{"*"}}, iam.ErrAccessDenied()},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if err := operation.call(tc.ac, target); !errors.Is(err, tc.want) {
						t.Fatalf("got %v, want %v", err, tc.want)
					}
				})
			}
		})
	}
}

func TestGrantCoverage(t *testing.T) {
	service := &RoleService{}
	if err := service.validateScopes([]string{"users:read"}, []string{"users:*"}); err != nil {
		t.Fatal(err)
	}
	if err := service.validateScopes([]string{"*"}, []string{"users:read"}); !errors.Is(err, role.ErrRoleInvalidScopes()) {
		t.Fatalf("broader grant accepted: %v", err)
	}
	if err := service.validateScopes([]string{"users:read"}, nil); !errors.Is(err, role.ErrRoleInvalidScopes()) {
		t.Fatalf("grant without authority accepted: %v", err)
	}
	if err := service.validateScopes([]string{"platform:tenants:read"}, []string{"*"}); err == nil {
		t.Fatal("platform grant accepted")
	}
}

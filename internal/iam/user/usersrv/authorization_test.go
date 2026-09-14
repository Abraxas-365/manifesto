package usersrv

import (
	"context"
	"errors"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"testing"
)

func TestServiceAuthorizationBeforeRepositories(t *testing.T) {
	service := &UserService{}
	for _, operation := range []struct {
		name string
		call func(*kernel.AuthContext, kernel.TenantID) error
	}{
		{"CreateUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.CreateUser(context.Background(), ac, user.CreateUserRequest{TenantID: target})
			return err
		}},
		{"GetUserByEmail", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetUserByEmail(context.Background(), ac, "value", target)
			return err
		}},
		{"GetUserByID", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetUserByID(context.Background(), ac, kernel.UserID("id"), target)
			return err
		}},
		{"GetUsersByTenant", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetUsersByTenant(context.Background(), ac, target)
			return err
		}},
		{"UpdateUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.UpdateUser(context.Background(), ac, kernel.UserID("id"), user.UpdateUserRequest{TenantID: target})
			return err
		}},
		{"ReinstateUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.ReinstateUser(context.Background(), ac, kernel.UserID("id"), target)
			return err
		}},
		{"SuspendUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.SuspendUser(context.Background(), ac, kernel.UserID("id"), target, "value")
			return err
		}},
		{"DeleteUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.DeleteUser(context.Background(), ac, kernel.UserID("id"), target)
			return err
		}},
		{"AddScopesToUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.AddScopesToUser(context.Background(), ac, kernel.UserID("id"), target, []string{"users:read"})
			return err
		}},
		{"RemoveScopesFromUser", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.RemoveScopesFromUser(context.Background(), ac, kernel.UserID("id"), target, []string{"users:read"})
			return err
		}},
		{"SetUserScopes", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.SetUserScopes(context.Background(), ac, kernel.UserID("id"), target, []string{"users:read"})
			return err
		}},
		{"GetUserScopes", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetUserScopes(context.Background(), ac, kernel.UserID("id"), target)
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
	service := &UserService{}
	if err := service.validateScopes([]string{"users:read"}, []string{"users:*"}); err != nil {
		t.Fatal(err)
	}
	if err := service.validateScopes([]string{"*"}, []string{"users:read"}); !errors.Is(err, user.ErrInsufficientScopes()) {
		t.Fatalf("broader grant accepted: %v", err)
	}
	if err := service.validateScopes([]string{"users:read"}, nil); !errors.Is(err, user.ErrInsufficientScopes()) {
		t.Fatalf("grant without authority accepted: %v", err)
	}
	if err := service.validateScopes([]string{"platform:tenants:read"}, []string{"*"}); err == nil {
		t.Fatal("platform grant accepted")
	}
}

func TestProfileUpdateRequiresScopeWriteForGrants(t *testing.T) {
	service := &UserService{}
	ac := &kernel.AuthContext{Actor: kernel.NewUserActor("user"), TenantID: "tenant", Scopes: []string{"users:write"}}
	if _, err := service.UpdateUser(context.Background(), ac, "user", user.UpdateUserRequest{TenantID: "tenant", Scopes: []string{}}); !errors.Is(err, iam.ErrAccessDenied()) {
		t.Fatal("scope replacement bypass", err)
	}
}

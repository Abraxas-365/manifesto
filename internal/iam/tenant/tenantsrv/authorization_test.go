package tenantsrv

import (
	"context"
	"errors"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"testing"
)

func TestServiceAuthorizationBeforeRepositories(t *testing.T) {
	service := &TenantService{}
	for _, operation := range []struct {
		name string
		call func(*kernel.AuthContext, kernel.TenantID) error
	}{
		{"GetTenantByID", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantByID(context.Background(), ac, target)
			return err
		}},
		{"GetTenantStats", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantStats(context.Background(), ac, target)
			return err
		}},
		{"GetTenantUsage", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantUsage(context.Background(), ac, target)
			return err
		}},
		{"GetTenantConfig", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantConfig(context.Background(), ac, target)
			return err
		}},
		{"SetTenantConfig", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.SetTenantConfig(context.Background(), ac, target, "value", "value")
			return err
		}},
		{"DeleteTenantConfig", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.DeleteTenantConfig(context.Background(), ac, target, "value")
			return err
		}},
		{"GetTenantUsers", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantUsers(context.Background(), ac, target)
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

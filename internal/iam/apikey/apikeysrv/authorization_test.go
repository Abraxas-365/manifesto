package apikeysrv

import (
	"context"
	"errors"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"testing"
)

func TestServiceAuthorizationBeforeRepositories(t *testing.T) {
	service := &APIKeyService{}
	for _, operation := range []struct {
		name string
		call func(*kernel.AuthContext, kernel.TenantID) error
	}{
		{"CreateAPIKey", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.CreateAPIKey(context.Background(), ac, target, kernel.UserID("id"), apikey.CreateAPIKeyRequest{})
			return err
		}},
		{"GetAPIKeyByID", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetAPIKeyByID(context.Background(), ac, kernel.APIKeyID("id"), target)
			return err
		}},
		{"GetTenantAPIKeys", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantAPIKeys(context.Background(), ac, target)
			return err
		}},
		{"UpdateAPIKey", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.UpdateAPIKey(context.Background(), ac, kernel.APIKeyID("id"), target, apikey.UpdateAPIKeyRequest{})
			return err
		}},
		{"RevokeAPIKey", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.RevokeAPIKey(context.Background(), ac, kernel.APIKeyID("id"), target)
			return err
		}},
		{"DeleteAPIKey", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.DeleteAPIKey(context.Background(), ac, kernel.APIKeyID("id"), target)
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
	service := &APIKeyService{}
	if err := service.validateScopes([]string{"users:read"}, []string{"users:*"}); err != nil {
		t.Fatal(err)
	}
	if err := service.validateScopes([]string{"*"}, []string{"users:read"}); !errors.Is(err, apikey.ErrAPIKeyInsufficientScope()) {
		t.Fatalf("broader grant accepted: %v", err)
	}
	if err := service.validateScopes([]string{"users:read"}, nil); !errors.Is(err, apikey.ErrAPIKeyInsufficientScope()) {
		t.Fatalf("grant without authority accepted: %v", err)
	}
	if err := service.validateScopes([]string{"platform:tenants:read"}, []string{"*"}); err == nil {
		t.Fatal("platform grant accepted")
	}
}

func TestCreationRequiresMatchingUserActor(t *testing.T) {
	service := &APIKeyService{}
	for _, actor := range []kernel.Actor{kernel.NewAPIKeyActor("key"), kernel.NewUserActor("other-user")} {
		ac := &kernel.AuthContext{Actor: actor, TenantID: "tenant", Scopes: []string{"*"}}
		if _, err := service.CreateAPIKey(context.Background(), ac, "tenant", "creator", apikey.CreateAPIKeyRequest{}); !errors.Is(err, iam.ErrAccessDenied()) {
			t.Fatal("creator attribution bypass", err)
		}
	}
}

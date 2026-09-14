package invitationsrv

import (
	"context"
	"errors"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/invitation"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"testing"
)

func TestServiceAuthorizationBeforeRepositories(t *testing.T) {
	service := &InvitationService{}
	for _, operation := range []struct {
		name string
		call func(*kernel.AuthContext, kernel.TenantID) error
	}{
		{"CreateInvitation", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.CreateInvitation(context.Background(), ac, target, kernel.UserID("id"), invitation.CreateInvitationRequest{})
			return err
		}},
		{"ResendInvitation", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.ResendInvitation(context.Background(), ac, kernel.InvitationID("id"), target)
			return err
		}},
		{"GetInvitationByID", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetInvitationByID(context.Background(), ac, kernel.InvitationID("id"), target)
			return err
		}},
		{"GetTenantInvitations", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetTenantInvitations(context.Background(), ac, target)
			return err
		}},
		{"GetPendingInvitations", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			_, err := service.GetPendingInvitations(context.Background(), ac, target)
			return err
		}},
		{"RevokeInvitation", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.RevokeInvitation(context.Background(), ac, kernel.InvitationID("id"), target)
			return err
		}},
		{"DeleteInvitation", func(ac *kernel.AuthContext, target kernel.TenantID) error {
			err := service.DeleteInvitation(context.Background(), ac, kernel.InvitationID("id"), target)
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
	service := &InvitationService{}
	if err := service.validateScopes([]string{"users:read"}, []string{"users:*"}); err != nil {
		t.Fatal(err)
	}
	if err := service.validateScopes([]string{"*"}, []string{"users:read"}); !errors.Is(err, iam.ErrAccessDenied()) {
		t.Fatalf("broader grant accepted: %v", err)
	}
	if err := service.validateScopes([]string{"users:read"}, nil); !errors.Is(err, iam.ErrAccessDenied()) {
		t.Fatalf("grant without authority accepted: %v", err)
	}
	if err := service.validateScopes([]string{"platform:tenants:read"}, []string{"*"}); err == nil {
		t.Fatal("platform grant accepted")
	}
}

func TestCreationRequiresMatchingUserActor(t *testing.T) {
	service := &InvitationService{}
	for _, actor := range []kernel.Actor{kernel.NewAPIKeyActor("key"), kernel.NewUserActor("other-user")} {
		ac := &kernel.AuthContext{Actor: actor, TenantID: "tenant", Scopes: []string{"*"}}
		if _, err := service.CreateInvitation(context.Background(), ac, "tenant", "creator", invitation.CreateInvitationRequest{}); !errors.Is(err, iam.ErrAccessDenied()) {
			t.Fatal("creator attribution bypass", err)
		}
	}
}

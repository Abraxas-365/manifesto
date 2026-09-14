package authz_test

import (
	"errors"
	"testing"

	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/authz"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

func TestRequire(t *testing.T) {
	tenant := kernel.NewTenantID("tenant")
	user := kernel.NewUserActor(kernel.NewUserID("user"))
	key := kernel.NewAPIKeyActor(kernel.NewAPIKeyID("key"))
	for _, tc := range []struct {
		name     string
		actor    kernel.Actor
		tenant   kernel.TenantID
		held     []string
		required string
		want     error
	}{
		{"exact", user, tenant, []string{"users:read"}, "users:read", nil},
		{"prefix wildcard", user, tenant, []string{"users:*"}, "users:read", nil},
		{"tenant wildcard", user, tenant, []string{"*"}, "users:read", nil},
		{"independent key", key, tenant, []string{"users:read"}, "users:read", nil},
		{"invalid actor", kernel.Actor{}, tenant, []string{"*"}, "users:read", iam.ErrUnauthorized()},
		{"missing actor tenant", user, "", []string{"*"}, "users:read", iam.ErrUnauthorized()},
		{"foreign tenant wildcard", user, "other", []string{"*"}, "users:read", iam.ErrAccessDenied()},
		{"missing permission", user, tenant, nil, "users:read", iam.ErrAccessDenied()},
		{"wrong permission", user, tenant, []string{"users:write"}, "users:read", iam.ErrAccessDenied()},
		{"empty permission", user, tenant, []string{"*"}, "", iam.ErrAccessDenied()},
		{"blank permission", user, tenant, []string{"*"}, "  ", iam.ErrAccessDenied()},
		{"platform with wildcard", user, tenant, []string{"*"}, "platform:tenants:read", iam.ErrAccessDenied()},
		{"legacy platform", user, tenant, []string{"platform:tenants:read"}, "platform:tenants:read", iam.ErrAccessDenied()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ac := &kernel.AuthContext{Actor: tc.actor, TenantID: tc.tenant, Scopes: tc.held}
			if err := authz.Require(ac, tenant, tc.required); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if err := authz.Require(nil, tenant, "users:read"); !errors.Is(err, iam.ErrUnauthorized()) {
		t.Fatal(err)
	}
	ac := &kernel.AuthContext{Actor: user, TenantID: tenant, Scopes: []string{"*"}}
	if err := authz.Require(ac, "", "users:read"); !errors.Is(err, iam.ErrAccessDenied()) {
		t.Fatal("empty target tenant allowed", err)
	}
}

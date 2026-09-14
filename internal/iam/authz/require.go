// Package authz provides shared authorization checks for trusted request contexts.
// It does not authenticate credentials or replace tenant-scoped repository queries.
package authz

import (
	"strings"

	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

// Require checks identity, tenant ownership and permission independently. Even
// the tenant wildcard cannot cross the tenant boundary. Callers must supply an
// AuthContext from trusted authentication, never from request JSON.
func Require(authCtx *kernel.AuthContext, tenantID kernel.TenantID, scope string) error {
	if authCtx == nil || !authCtx.IsValid() {
		return iam.ErrUnauthorized()
	}
	if tenantID.IsEmpty() || authCtx.TenantID != tenantID || strings.TrimSpace(scope) == "" || !authCtx.HasScope(scope) {
		return iam.ErrAccessDenied()
	}
	return nil
}

# 005 — Explicit service authorization

**Status:** Accepted; extends the delegation policy in [003](003-tenant-identity-and-lifecycle.md)

## Context

HTTP scope middleware cannot protect a service invoked directly by another adapter. Separately supplied caller scope slices also make it easy to omit checks or pass inconsistent authority. Tenant ownership and scope authority must be independent: `*` must never cross tenants.

## Decision

Add `internal/iam/authz.Require(authCtx, tenantID, scope)`. It returns IAM unauthorized for absent/invalid identity and access denied for tenant mismatch, empty tenant/scope, or missing permission. It uses kernel scope matching, including rejection of platform grants. Authentication remains in `auth`; the helper has no Fiber, repository, or container dependencies.

Customer-facing IAM services take an explicit `*kernel.AuthContext` immediately after `context.Context`, plus the target tenant (or existing tenant-bearing DTO). They authorize before validation or repository access, using the same scope constant as the route. HTTP middleware and repository tenant constraints remain in place.

Grant authority comes only from `authCtx.Scopes`: key grants, role create/update/assignment, direct user grants, and invitation direct/role grants must be covered by the caller. User profile updates containing scopes additionally require `scopes:write`. API-key and invitation creation require a user actor matching creator/inviter attribution. Scope removal may clean up retired grants and does not require possessing the revoked permission.

## Alternatives

- **Middleware only:** leaves non-HTTP entry points unprotected.
- **Ambient authorization in context values:** hides a mandatory security dependency.
- **A universal policy service with infrastructure:** unnecessary for deterministic tenant/scope checks.
- **Put IAM errors in kernel:** reverses the shared package dependency boundary.

## Consequences and limits

- This is a breaking Go service API change. Pass a trusted authentication context and remove separate caller scope arguments; HTTP DTOs and routes are unchanged. No migration is required.
- `AuthContext` is trusted in-process input, not an unforgeable capability. Never bind it from request JSON or construct a wildcard context to bypass a check.
- Authentication-time key validation, scope resolution, invitation-token inspection, and verified onboarding cannot require an already-authenticated context. Trusted provisioning, cross-tenant tenant administration, and invitation cleanup remain internal-only. Unwired operator HTTP handlers were removed; no operator app was added.
- The unused generic user-creation service now requires tenant authorization and covered grants, but remains unregistered. Public membership still comes from verified invitations.
- The helper does not refresh JWT scopes, validate live sessions, implement resource-specific ownership, or provide transactional grant locking. Existing token freshness and concurrent-write limitations remain; this change is not a concurrency/security clearance.
- Tests exercise the helper, every protected service's early rejection paths, grant attenuation, and user-only creation. Database integration behavior is not changed by the helper.

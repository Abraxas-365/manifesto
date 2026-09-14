# 003 — Tenant identity and lifecycle

**Status:** Accepted; operator application intentionally outside scope

## Context

A tenant administrator must be able to manage application access without gaining authority over other customers. Machine credentials must remain identifiable without impersonating their creators. Generic CRUD updates must not bypass invitation acceptance or resurrect revoked credentials.

## Decision

### Separate application and platform authority

Customer IAM owns application scopes inside a tenant. `*` remains tenant-local. `platform:` permissions are not assignable or effective in this customer application, and cross-tenant routes are not registered.

A future operator application needs its own authentication boundary. Do not treat a hidden catalog entry or a specially named scope as sufficient isolation for platform authority.

### Identify the actual actor

An authenticated request is made by a user or an API key. Use `kernel.Actor` with private IDs, checked accessors, and an invalid zero value. A key's creator is attribution, not the principal exercising the key's authority.

This small struct is a Go approximation of an exclusive choice, not a compiler-enforced algebraic sum type. Authentication boundaries validate it.

### Express lifecycle transitions as operations

Users enter through verified invitation onboarding, not public generic user creation. Returning login is distinct from membership creation or provider linking. Generic user updates cannot activate a pending member; generic key updates cannot reactivate a revoked secret.

Use explicit suspension, reinstatement, and revocation operations. API-key scope creation/replacement is restricted to scopes covered by the caller's authenticated grants.

## Alternatives

- **Tenant `*` also means platform administrator:** convenient but collapses the customer/platform security boundary.
- **Use the key creator's user identity for every key request:** simple attribution but misrepresents the credential that acted and conflates independent key lifecycles with user sessions.
- **Single generic status update endpoint:** fewer routes, but makes security-sensitive transitions indistinguishable from metadata edits.
- **Automatically link OAuth accounts by email:** easy onboarding, but trusts an attribute without sufficient proof of identity ownership.

## Consequences

- Every resource lookup still needs tenant isolation; a wildcard matcher cannot enforce database ownership.
- Key creation and invitation creation currently require user actors, while other authorized key operations can use key actors.
- Suspending a creator does not revoke independent API keys. Suspending a tenant blocks their authentication.
- Verified-email requirements can prevent otherwise convenient onboarding. New Microsoft invitation linking is currently blocked until verification is implemented.
- JWT grant changes are not instantly refreshed, and the API-key subset policy is not yet a universal delegation rule for every IAM grant path.
- Tenant provisioning and first-administrator bootstrap remain trusted application-specific operations.

See [IAM](../iam.md) for the concrete request flows and limitations.

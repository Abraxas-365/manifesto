# 004 — Persistence and credential invalidation

**Status:** Implemented in PostgreSQL; database integration validation remains necessary

## Context

Onboarding changes several records that must agree: membership, grants, roles, tenant capacity, and invitation status. Separately, checking that a user is currently active is insufficient to invalidate an old token: reinstating that user would make the same token usable again. Deleting sessions alone also does not cover legacy sessionless JWTs.

## Decision

### Explicit transaction boundaries

The invitation acceptance port commits the complete onboarding change or rolls it back. Its PostgreSQL adapter explicitly injects the transaction into repositories through `iaminfra.DBTX`; it does not discover a transaction hidden in `context.Value`.

The adapter locks invitation and tenant records and validates role association before commit. Conditional invitation writes protect acceptance from stale revocation/deletion. User deletion combines membership removal and tenant-count decrement in a SQL statement coordinated with the tenant lock.

This is not a generic unit-of-work framework. Keep the consistency boundary visible and specific to the use case.

### Persist credential generations

Users and refresh tokens have a `credential_version`; access JWTs carry the same generation. Authentication compares the presented generation with the current persisted user.

Migration `005` installs a PostgreSQL trigger that, on transition to `SUSPENDED`:

1. Advances the user's credential generation.
2. Revokes their refresh tokens.
3. Expires their sessions.

These effects commit with the status change. Other updates preserve the generation, and repository updates use a version predicate to reject stale saves. Reinstatement does not reset the generation. Credentials issued from an older snapshot remain invalid even if issuance races suspension.

## Alternatives

- **Best-effort sequential onboarding writes:** simpler, but partial failures can grant access without a consistent acceptance record.
- **Ambient transaction in context:** avoids constructor plumbing but hides persistence behavior and couples callers to implicit conventions.
- **Wait for access-token expiry:** avoids request-time version checks but leaves a revocation window.
- **Delete sessions only:** does not invalidate sessionless JWTs or all racing credentials.
- **Only test current user status:** blocks access while suspended but revives old credentials after reinstatement.
- **Application-only invalidation:** can fit ports and adapters more cleanly, but every status-changing write must participate in the same atomic protocol. The current implementation places this invariant in PostgreSQL instead.

## Consequences

- JWT requests require current user/tenant reads and, when applicable, a session read; authentication is not stateless.
- Migration rollout must precede new code, and older binaries must not remain serving authentication during the transition.
- Suspension behavior is PostgreSQL-specific. Another adapter must explicitly reproduce the invariant; the interface alone does not enforce it.
- The trigger is hidden behavior from the perspective of a casual `Save` caller. Document it in migrations and adapter contracts, and test status transitions against a real database.
- OTP consumption is outside the acceptance transaction. If acceptance fails after code verification, a new code is required.
- Notification delivery is not part of the transaction, and no delivery outbox is implemented in this flow.

See [operations](../operations.md) for migration compatibility and required concurrency tests.

# Identity and access management

[Documentation index](README.md)

## Tenant boundary

Customer IAM manages permissions inside an authenticated tenant. A scope answers **what action is allowed**; the tenant constraint answers **which resources are accessible**. Both checks are required.

- `*` is full application authority within the caller's tenant, not cross-tenant authority.
- `platform:` scopes are rejected by customer grant validation and scope matching, including legacy stored values.
- Customer routes expose `/tenants/me`, not cross-tenant `/admin/tenants` operations.
- Internal tenant-management services and unregistered handlers still exist. Their existence is not authorization to expose them.

A future operator application must have its own authentication boundary. It is not implemented here.

## Actors: users and API keys

[AuthContext](../internal/kernel/context.go) carries an [Actor](../internal/kernel/actor.go), tenant, optional session, identity details, and scopes. An actor identifies exactly one user or API key.

```go
userID, isUser := authContext.Actor.UserID()
keyID, isKey := authContext.Actor.APIKeyID()
```

Use checked accessors rather than inferring the actor from an optional creator/user association. The zero-value actor is invalid. Its JSON representation is `{ "type": "user", "id": "..." }` or `{ "type": "api_key", "id": "..." }`.

An API key acts as itself, not as the person who created it. Suspending a creator does not revoke independent keys. Persistent API-key creator attribution and a general resource audit-log store are not implemented; the auth audit adapter currently logs events.

## Scopes and roles

[Scope constants](../internal/iam/scopes/scopes.go) define the application permission catalog. Supported matching includes exact scopes, prefix wildcards such as `users:*`, and tenant-local `*`. Use constants in route registration.

A user's effective scopes combine direct grants and assigned roles. The scope resolver is used during token issuance/refresh; **JWT resource requests currently use the scopes embedded in the token**, not freshly resolved role permissions. Removing a role or grant therefore may not affect an existing access token until expiry.

API-key creation and scope replacement require every requested scope to be covered by the caller's authenticated scopes:

| Caller holds | Requested key scope | Allowed |
| --- | --- | --- |
| `users:*` | `users:read` | Yes |
| `users:read` | `users:*` | No |
| `api_keys:write` | `*` | No |
| `*` | `platform:tenants:read` | No |

The subset check is specific to API-key grants. It is not a universal permission-delegation policy for roles, direct user grants, and invitations. Those management permissions are privileged and must be assigned accordingly.

## Authentication policies

[UnifiedAuthMiddleware](../internal/iam/auth/unified_middleware.go) offers two policies:

- `Authenticate()` accepts API keys or JWTs and retains their scopes for resource authorization. Recognized API-key credentials take precedence.
- `AuthenticateUserJWT()` protects user/session endpoints using JWT identity without granting scope authority. An API key alone cannot establish that identity.

JWT checks include token validity, current user and tenant eligibility, credential version, and session ownership/expiry when a session ID exists. Legacy sessionless JWTs still receive user, tenant, and credential-version checks. A nonempty Bearer credential takes precedence over the access-token cookie; an invalid Bearer token does not fall back to the cookie.

API-key authentication checks format, stored hash, revocation/expiry, and tenant activity. Secrets are cryptographically random and stored as SHA-256 hashes, not bcrypt hashes. The raw key is returned only on creation. Use `X-API-Key`; do not put secrets in URLs even though legacy query-string extraction remains supported.

## Onboarding is a workflow

There is no public `POST /users`. Creating a tenant member and granting access requires verified onboarding.

### Invitations

An authorized user creates an invitation containing an email and direct scopes and/or a role. Eligible pending/active members may also receive invitations to finish onboarding or link a supported authentication method. Suspended/inactive members and duplicate pending invitations are rejected.

Responses omit the invitation token. Delivery is through an injected notifier; delivery failure can leave a pending invitation, which can be listed and resent. Accepted invitations cannot be reused for login or another grant operation.

### OAuth

The application initiates login, persists state, and handles the provider callback. Without an invitation, returning login must match an existing provider identity. It does not link accounts merely because their email strings match. Optional tenant selection disambiguates multiple matching memberships.

Invitation acceptance/linking requires provider-proven email verification and respects the selected tenant. Google and Microsoft adapters are wired conditionally in the current container. Microsoft Graph mail/UPN does not prove mailbox ownership: returning linked Microsoft accounts can log in, but new Microsoft invitation linking is rejected until a trusted verification flow is implemented.

### Passwordless

1. Signup initiation validates the invitation and sends a `SIGNUP` OTP. It does not create membership or grant access.
2. Signup verification requires `email`, `name`, `tenant_id`, `invitation_token`, and `code`.
3. After email verification, invitation acceptance commits membership, authentication method, scopes, role assignment, tenant count, and acceptance in one PostgreSQL transaction.
4. The user then logs in separately with a `LOGIN` OTP.

Signup resend requires `invitation_token`, `email`, `tenant_id`, and `purpose: "signup"`. Login verification and resend require an eligible OTP-enabled member. OTP consumption occurs before the acceptance transaction: if acceptance fails, request another signup code before retrying.

## Lifecycle operations

- Generic user updates reject `status`. Suspension and reinstatement are explicit operations; pending-user activation belongs to onboarding.
- Suspension advances the persisted credential version, revokes refresh tokens, and expires sessions in the same database transaction as the status change. Reinstatement does not restore old credentials.
- Generic API-key updates reject `is_active`. Revocation is irreversible for the existing secret, including stale repository writes.
- An explicit empty direct-scope array clears direct grants; it does not remove permissions inherited from roles.

See the [persistence decision](decisions/004-persistence-and-credential-invalidation.md) and [migration notes](operations.md).

## HTTP surface

All paths below are relative to `/api/v1`. The source registration is authoritative; `/api/v1/docs` currently returns an empty endpoint map, not an OpenAPI specification.

### Authentication

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/auth/login` | Initiate OAuth login |
| GET | `/auth/callback/:provider` | OAuth callback |
| POST | `/auth/refresh` | Rotate refresh credentials |
| GET | `/auth/me`, `/auth/sessions` | User/session information; user JWT required |
| POST | `/auth/logout`, `/auth/logout/all` | Session logout; user JWT required |
| POST | `/auth/passwordless/tenants` | Membership discovery |
| POST | `/auth/passwordless/signup/initiate`, `/auth/passwordless/signup/verify` | Invitation-based signup |
| POST | `/auth/passwordless/login/initiate`, `/auth/passwordless/login/verify` | OTP login |
| POST | `/auth/passwordless/resend-otp` | Resend signup/login code |

### Tenant management

| Resource | Operations | Scope family |
| --- | --- | --- |
| `/users` | List; get/update/delete `/:id`; POST `/:id/suspend`, `/:id/reinstate` | `users:*` |
| `/users/:id/scopes` | GET, PUT replacement, POST addition, DELETE removal | `scopes:*` |
| `/roles` | List/create; get/update/delete `/:id` | `roles:*` |
| `/roles/:id/users` | POST assignment; DELETE `/:userID` unassignment | `roles:assign` |
| `/users/:id/roles` | GET effective scopes and roles | `roles:read` |
| `/api-keys` | List/create; get/update/delete `/:id`; POST `/:id/revoke` | `api_keys:*` |
| `/invitations` | List/create; get/delete `/:id`; POST `/:id/resend`, `/:id/revoke` | `invitations:*` |
| `/scopes` | GET catalog | `scopes:read` |
| `/tenants/me` | GET own tenant, `/stats`, `/usage` | `tenants:read` |
| `/tenants/me/config` | GET, PUT; DELETE `/:key` | `tenants:config` |

Scope families summarize the area; each route requires its specific read/write/delete/assign/revoke constant. User updates carrying scopes additionally require `scopes:write`. Key and invitation creation require a user actor. Consult each `*api/handler.go` for exact DTOs and response behavior.

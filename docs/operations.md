# Operations and limitations

[Documentation index](README.md)

Manifesto supplies implementation building blocks, not a completed production operating model. Review these requirements for every consuming application.

## Schema and upgrades

The [migrations directory](../migrations) contains ordered PostgreSQL scripts:

| Migration | Purpose |
| --- | --- |
| `001_genesis.up.sql` | Initial IAM tables, constraints, and indexes |
| `002_invitation_role_id.up.sql` | Optional role association on invitations |
| `003_session_refresh_linking.up.sql` | Refresh-token/session linkage |
| `004_auth_otp_purposes.up.sql` | Separate signup and login OTP purposes |
| `005_suspension_credentials.up.sql` | Credential versions and transactional suspension invalidation |

**Fresh-install blocker:** migration `003` adds a UUID session foreign key referencing the VARCHAR session ID in migration `001`. The types must be reconciled in a reviewed schema change before the current migration sequence can complete. If using per-file transactions, files `001`–`002` remain applied when `003` fails. Do not blindly restart the entire sequence.

Use a version-tracking migration procedure for existing databases. Back up data, rehearse upgrades against a disposable PostgreSQL instance, and plan recovery before applying schema changes. The application does not automatically apply these scripts. No down migrations are supplied for these changes.

### Authentication upgrade compatibility

- Migration `004` must precede code using `SIGNUP`/`LOGIN` purposes. Existing `VERIFICATION` codes cannot be used for the new signup/login flow; request fresh codes.
- Migration `005` must precede code querying credential versions. Stop older application instances during the rollout because they do not enforce version comparisons.
- Active users start at generation zero for legacy-token compatibility. Already suspended users are invalidated by the migration. Reinstatement preserves the advanced generation.
- Signup verification requires `name` and `invitation_token` in addition to email, tenant, and code. Signup resend also requires the invitation token.
- Generic user `status` and key `is_active` updates are rejected; clients must use explicit lifecycle endpoints.

A replacement persistence adapter must reproduce the suspension trigger's invariants. Merely satisfying the repository interface is insufficient.

## Before exposing the application

- Replace development secrets, require TLS, restrict CORS, and configure secure cookie behavior. Review CSRF protections for your deployment.
- Wire real OTP and invitation delivery through the injected notification ports. Setting `EMAIL_PROVIDER` alone does not change the current root composition.
- Remove the console OTP notifier from production: it currently logs codes without an environment guard. The invitation console notifier prints tokens only in development and returns an error outside development.
- Define a trusted first-tenant/administrator bootstrap process. No working seed is included, and customer IAM does not expose tenant provisioning.
- Review actor/tenant enforcement on every application route; scope matching alone cannot prevent cross-tenant access.
- Assign role/user-scope/invitation management capabilities only to trusted administrators. Only API-key grant paths currently enforce the caller-scope subset policy.
- Choose a log-retention and durable auditing strategy. The existing auth audit adapter writes logs; it is not a complete audit store for resource changes.
- Verify PostgreSQL/Redis backups, retention, resource limits, and outage behavior. Do not treat the supplied Compose configuration as production infrastructure.

## Known implementation limits

| Area | Current behavior / action required |
| --- | --- |
| JWT permissions | Scopes are embedded at issuance; removing grants is not immediately reflected on every resource request |
| API-key transport | Query-string extraction remains supported; use headers and prevent secrets from entering URL logs |
| API-key configuration | Format validation expects a 64-character hex secret while generation length is configurable; retain the default 32-byte length until aligned |
| API-key attribution | Keys are independent actors; a mandatory persisted `CreatedBy` field is not implemented |
| Microsoft onboarding | Existing linked identities can log in; new invitation linking requires a mailbox-verification mechanism not yet implemented |
| Notification reliability | Failed invitation sends leave pending records available for resend; there is no transactional delivery outbox in this flow |
| OTP and acceptance | OTP consumption is outside the acceptance transaction; a failed acceptance requires a new code |
| Operator administration | A separate operator application is not implemented; customer cross-tenant routes are not registered |
| Development tooling | `make migrate` points to a missing filename; dependent setup/reset targets need correction before use |
| Local Redis startup | The empty-password Compose command leaves `--requirepass` without an argument; use the documented nonempty local override and authenticated readiness check |
| API specification | `/api/v1/docs` is a placeholder, not generated or complete endpoint documentation |
| Request tracing | The reference request-ID fallback generator is deterministic; replace it before relying on unique request correlation |

## Verification strategy

Compilation and the current unit tests are useful checks, but do not establish database or workflow correctness. Many service/adapter packages have no tests yet. In particular, validate:

- Cross-tenant access attempts for users, roles, invitations, and keys.
- Old JWT and refresh-token rejection after suspension and reinstatement, including sessionless tokens and concurrent issuance.
- Rejection of API keys for inactive tenants and of grants beyond the caller's scopes.
- Invitation rollback on role-assignment failure and competing acceptance/revocation/deletion.
- Tenant user-count consistency under concurrent membership changes.
- OTP expiry, attempt limits, resend eligibility, and separation of signup/login purposes.
- Migration compatibility and failure recovery against actual PostgreSQL.

Do not interpret the documented controls as evidence of a completed independent security audit or database integration validation.

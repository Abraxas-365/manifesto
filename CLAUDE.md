# Manifesto

Manifesto is an opinionated Go framework for building multi-tenant SaaS backends. It provides a complete IAM system, utility packages, and a CLI that scaffolds new projects by copying modules into the target codebase (not imported as a library). Every project owns its own copy of the code under `internal/`, with the import path rewritten to match the project module.

## Architecture Philosophy

**Hexagonal / Ports & Adapters.** Every domain module follows the same three-layer structure:

```
internal/<module>/
    <entity>.go          # Domain: entities, value objects, DTOs, error registry
    port.go              # Domain: interfaces (ports) that infra must implement
    <module>srv/          # Application: service layer (use cases, business logic)
    <module>infra/        # Infrastructure: concrete implementations (Postgres, Redis, etc.)
    <module>api/          # Interface: HTTP handlers (Fiber)
```

Dependencies always point inward: `api -> srv -> domain <- infra`. The service layer depends only on interfaces defined in the domain package. Infrastructure implements those interfaces. Handlers call services.

**No magic, no reflection, no DI framework.** Wiring is done explicitly in container files using plain constructor injection.

## Project Structure

```
cmd/
    container.go         # Root composition root - wires infrastructure + modules
    server.go            # Fiber app setup, middleware, route registration
internal/
    config/              # Environment-based configuration (env vars)
    kernel/              # Shared types: typed IDs, AuthContext, BindAndValidate, Store
    errx/                # Structured error system with registries, codes, and HTTP status mapping
    logx/                # Structured logger with levels, formatters (console/JSON)
    asyncx/              # Concurrency primitives: Map, Pool, Batch, Debounce, Pipeline, Stream
    fsx/                 # File system abstraction (local + S3 implementations)
    notifx/              # Email notification abstraction (console + SES implementations)
    jobx/                # Background job queue (Redis-backed, retries, delayed jobs)
    ptrx/                # Pointer/optional helpers: Ptr[T], Val[T], Coalesce, Map, Filter
    iam/                 # Identity & Access Management (see below)
    ai/                  # AI provider abstraction and harness
manifesto.yaml           # Module manifest - tracks which manifesto modules are installed
migrations/              # SQL migrations (Postgres)
```

## Kernel Package

The `internal/kernel/` package contains shared types and utilities used across all modules. It is the only package that every module may depend on — it has zero dependencies on any domain module.

### Typed IDs (`common_ids.go`)

All domain entity IDs use typed string wrappers. This prevents accidentally passing a `UserID` where a `TenantID` is expected:

```go
kernel.UserID         // User identity
kernel.TenantID       // Tenant identity
kernel.RoleID         // Role identity
kernel.InvitationID   // Invitation identity
kernel.APIKeyID       // API key identity

// Construction and usage
id := kernel.NewUserID("uuid-here")
id.String()   // → "uuid-here"
id.IsEmpty()  // → false
```

Handlers convert URL params immediately: `userID := kernel.NewUserID(c.Params("id"))`. Raw `string` must never flow through service or domain layers for entity IDs.

### Request Binding and Validation (`bind.go`)

`BindAndValidate[T]` is a generic helper that parses the request body and calls the struct's `Validate()` method in one step. Every request DTO must implement `Validate() error` via a pointer receiver — the compiler enforces this:

```go
func (h *UserHandlers) CreateUser(c *fiber.Ctx) error {
    req, err := kernel.BindAndValidate[user.CreateUserRequest](c)
    if err != nil {
        return err  // returns errx.Validation automatically
    }
    // req is parsed and validated
}
```

Request DTOs use explicit `Validate()` methods instead of struct tags (e.g., no `validate:"required"`). This keeps validation logic visible and testable:

```go
type CreateUserRequest struct {
    Email  string   `json:"email"`
    Name   string   `json:"name"`
    Scopes []string `json:"scopes"`
}

func (r *CreateUserRequest) Validate() error {
    if r.Email == "" {
        return errx.Validation("email is required")
    }
    // ...
    return nil
}
```

### Key-Value Store Abstraction (`store.go`)

`kernel.Store[T]` provides a generic interface for simple key-value storage. Modules that need lightweight persistence (configuration, feature flags, caches) depend on this interface rather than importing specific infrastructure:

```go
type Store[T any] interface {
    Get(ctx context.Context, key string) (T, error)
    Set(ctx context.Context, key string, value T) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
}
```

### Auth Context (`context.go`)

`kernel.AuthContext` carries the authenticated user's identity and scopes through every request. Injected by the auth middleware:

```go
type AuthContext struct {
    UserID   *UserID  `json:"user_id"`
    TenantID TenantID `json:"tenant_id"`
    Email    string   `json:"email"`
    Name     string   `json:"name"`
    Scopes   []string `json:"scopes"`
    IsAPIKey bool     `json:"is_api_key"`
}
```

Scope matching supports exact match, global wildcard `*`, and prefix wildcards (`users:*` matches `users:read`). Use `kernel.MatchScope()`, `kernel.ScopesContain()`, or `AuthContext.HasScope()`.

## Container Pattern

The app uses a two-level container hierarchy:

1. **Root Container** (`cmd/container.go`): Owns shared infrastructure (DB, Redis, FileSystem) and composes bounded-context containers.
2. **Module Containers** (e.g., `internal/iam/iamcontainer/container.go`): Each bounded context wires its own dependency graph. Receives infrastructure through an explicit `Deps` struct -- no globals, no ambient state.

Construction order within a module container: **infra (repos) -> services -> handlers -> middleware**.

```go
// cmd/container.go
c.IAM = iamcontainer.New(iamcontainer.Deps{
    DB:    c.DB,
    Redis: c.Redis,
    Cfg:   c.Config,
})
```

## Error Handling (errx)

Manifesto uses a registry-based error system. Each module declares an `ErrRegistry` with a prefix and registers error codes with type, HTTP status, and message:

```go
var ErrRegistry = errx.NewRegistry("IAM")
var CodeUnauthorized = ErrRegistry.Register("UNAUTHORIZED", errx.TypeAuthorization, http.StatusUnauthorized, "Unauthorized")

// Usage
return iam.ErrUnauthorized()                          // [IAM_UNAUTHORIZED] Unauthorized
return iam.ErrUnauthorized().WithDetail("reason", x)  // with structured details
return errx.Wrap(err, "context message", errx.TypeInternal) // wraps preserving original error code
```

Error types: `TypeValidation` (400), `TypeAuthorization` (401), `TypeNotFound` (404), `TypeConflict` (409), `TypeBusiness` (422), `TypeExternal` (502), `TypeInternal` (500).

### Error Comparison with `errors.Is`

`errx.Error` implements the `Is(target error) bool` method, matching by error **code** rather than pointer identity. This means `errors.Is` works correctly even though each `ErrXxx()` constructor returns a new pointer:

```go
errors.Is(err, role.ErrRoleNotFound())  // true if err has the same code
```

Additionally, `errx.IsNotFound(err)` checks whether any error in the chain is an `errx.Error` with `TypeNotFound` — useful for generic "not found" handling without importing a specific module's error.

### Repository "Not Found" Pattern

Go has no `Option<T>` type, so unlike Rust's `fetch_optional` which returns `None`, Go's `database/sql` signals "no rows" with `sql.ErrNoRows`. The idiomatic Go approach — used by Kubernetes, GORM, and the Go stdlib — is to **translate `sql.ErrNoRows` into a domain error at the repository boundary**:

```go
// Repository layer — translates sql.ErrNoRows into a domain error
func (r *PostgresRoleRepository) GetByID(ctx context.Context, id kernel.RoleID) (*role.Role, error) {
    var p rolePersistence
    err := r.db.GetContext(ctx, &p, query, id.String())
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, role.ErrRoleNotFound()  // domain error, not sql error
        }
        return nil, errx.Wrap(err, "failed to find role", errx.TypeInternal)
    }
    d := toDomain(p)
    return &d, nil
}
```

**Never swallow errors with `_`** when calling a repo method. When "not found" is an expected outcome (e.g., checking for duplicates before creation), use `errors.Is` to distinguish it from real database errors:

```go
// Service layer — "not found" is expected, real DB errors are not
existing, err := s.roleRepo.GetByName(ctx, req.Name, tenantID)
if err != nil && !errors.Is(err, role.ErrRoleNotFound()) {
    return nil, errx.Wrap(err, "failed to check role name", errx.TypeInternal)
}
if existing != nil {
    return nil, role.ErrRoleAlreadyExists()
}
```

Why not `nil, nil` (like Rust's `Option`)?
- Go has no compiler-enforced `Option<T>` — a caller who forgets to check `nil` gets a runtime panic, not a compile error
- `nil, nil` is ambiguous: does it mean "not found" or "success with no data"?
- Returning a typed domain error makes the "not found" case explicit and grep-able

## IAM Module

The IAM module is the largest module and provides complete multi-tenant auth:

### Multi-Tenancy

Every entity belongs to a tenant. `kernel.TenantID` and `kernel.UserID` are typed strings used everywhere. Users are scoped to tenants (unique email per tenant). The first user to sign up creates a new tenant and gets the `*` (superadmin) scope.

### Authentication Methods

**OAuth** (`internal/iam/auth/handlers.go`): Google, Microsoft, Auth0. The flow:
1. `GET /api/v1/auth/{provider}/login` - Returns OAuth redirect URL (state stored in Redis)
2. `GET /api/v1/auth/{provider}/callback` - Exchanges code for tokens, creates/finds user, issues JWT + refresh token
3. If user has a pending invitation, it's automatically accepted on first login

**Passwordless/OTP** (`internal/iam/auth/passwordless_handlers.go`): Email-based magic link flow:
1. `POST /api/v1/auth/passwordless/initiate` - Sends OTP code to email
2. `POST /api/v1/auth/passwordless/verify` - Verifies OTP, creates/finds user, issues JWT

The OTP system (`internal/iam/otp/`):
- Cryptographically secure random codes (configurable length, default 6 digits)
- Rate limiting (configurable window between requests)
- Max attempts tracking (configurable, default 5)
- Expiration (configurable TTL)
- Stored in Postgres, sent via the `NotificationService` interface (inject SES, console logger, etc.)

### Token System

- **Access tokens**: Short-lived JWTs containing user_id, tenant_id, email, name, scopes
- **Refresh tokens**: Long-lived, stored in Postgres, used to get new access tokens
- **Sessions**: Track active sessions with IP, user agent, last activity
- Token refresh: `POST /api/v1/auth/refresh`
- Logout: `POST /api/v1/auth/logout` (revokes refresh token + session)

### Scope-Based Authorization

Permissions use a flat scope system inspired by OAuth scopes, not traditional RBAC roles:

```
users:read, users:write, users:delete
roles:read, roles:write, roles:assign
tenants:read, tenants:write
platform:tenants:read, platform:tenants:write
```

Wildcard support:
- `*` grants access to everything (superadmin)
- `users:*` grants all `users:` scopes
- Matching logic is in `kernel.MatchScope()` and `kernel.ScopesContain()`

Scopes are defined in `internal/iam/scopes/scopes.go`:
- **Tenant scopes** — used by tenant admins to manage their own tenant: `users:*`, `roles:*`, `scopes:*`, `tenants:*`, `api_keys:*`, `invitations:*`
- **Platform scopes** — reserved for platform operators: `platform:tenants:*` (read, write, delete, config, suspend)

All scope constants live in `scopes.go` as typed constants (e.g., `scopes.ScopeUsersRead`). Handler route registration must use these constants, never hardcoded strings:

```go
// Correct
tenants.Get("/", authMiddleware.RequireScope(iamscopes.ScopeTenantsRead), h.GetMyTenant)

// Wrong — never do this
tenants.Get("/", authMiddleware.RequireScope("tenants:read"), h.GetMyTenant)
```

The scope catalog endpoint (`GET /api/v1/scopes/`) returns scopes grouped by category with descriptions, used by admin UIs to render permission checkboxes when creating roles.

### Platform Scope Protection

Scopes prefixed with `platform:` are reserved for platform operators and **cannot be assigned by regular tenant admins**. This is enforced at three levels:

1. **Scope assignment** — `usersrv.AddScopesToUser`, `usersrv.SetUserScopes`, `rolesrv.CreateRole`, `rolesrv.UpdateRole`, `apikeysrv.CreateAPIKey`, `apikeysrv.UpdateAPIKey` all reject platform scopes unless the caller themselves holds a platform scope.
2. **Scope catalog** — `GET /api/v1/scopes/` hides the "Platform: Tenants" category entirely from non-platform callers.
3. **Scope validation helpers** in `scopes/scope_manager.go`:
   - `IsPlatformScope(scope)` — checks `platform:` prefix
   - `ContainsPlatformScope(scopes)` — checks if any scope in a slice is platform-reserved
   - `CallerHasPlatformScope(callerScopes)` — checks if caller has `*` or any `platform:` scope
   - `FilterNonPlatformCategories()` — returns scope catalog without platform categories

To add new platform scopes (e.g., `platform:users:*` for cross-tenant user management), add them to `scopes.go` with the `platform:` prefix and they automatically inherit these protections.

### Roles

Roles (`internal/iam/role/`) are named collections of scopes assigned to users, similar to AWS IAM policies:

```go
type Role struct {
    ID          kernel.RoleID
    TenantID    kernel.TenantID
    Name        string
    Description string
    Scopes      []string
}
```

A user's **effective scopes** = their direct scopes + all scopes from assigned roles. The `ScopeResolver` interface computes this, and the auth middleware resolves it on every request.

CRUD: create roles, assign/unassign to users, list user roles and effective scopes.

### Unified Auth Middleware

`auth.UnifiedAuthMiddleware` handles both JWT and API key authentication:
1. Checks `Authorization: Bearer <jwt>` header first
2. Falls back to `X-API-Key: <key>` header
3. Resolves effective scopes (direct + role scopes)
4. Injects `kernel.AuthContext` into Fiber context

Use `middleware.RequireScope(iamscopes.ScopeXxx)` on route groups for authorization.

### API Keys

API keys (`internal/iam/apikey/`) are tenant-scoped, have their own scopes, and can be created by users:
- Keys are hashed (bcrypt) before storage; the raw key is returned only on creation
- Keys have optional expiration
- Keys carry scopes that limit what they can do
- Identified by prefix for easy lookup

### Invitations

The invitation system (`internal/iam/invitation/`) handles tenant user onboarding:
1. Admin creates invitation with email + scopes + optional role
2. Email sent via `NotificationService` interface
3. When invited user signs up (OAuth or passwordless), invitation is auto-accepted
4. User receives the scopes and role from the invitation

### Tenant Management: Platform Admin vs Self-Service

Tenant routes are split into two groups with different authorization:

**Platform admin routes** (`/api/v1/admin/tenants/...`) — cross-tenant operations for platform operators. Protected by `platform:tenants:*` scopes. These take `:id` from the URL to operate on any tenant:
- Create, list all, get/update/delete any tenant
- Suspend/activate any tenant
- Upgrade any tenant's plan
- View stats/usage/users/config for any tenant

**Tenant self-service routes** (`/api/v1/tenants/me/...`) — tenant owners manage their own tenant. Protected by `tenants:read`/`tenants:config` scopes. These pull the tenant ID from the JWT (`authContext.TenantID`), so a tenant owner can only see their own data:
- `GET /tenants/me` — own tenant info
- `GET /tenants/me/stats`, `/usage` — own metrics
- `GET/PUT/DELETE /tenants/me/config` — own configuration

Both route groups share the same `TenantService` — the difference is authorization and how the tenant ID is resolved (URL param vs JWT).

### User Management

- **Users** (`internal/iam/user/`): CRUD, scope management, status management (activate/suspend)
- All user operations are tenant-scoped via `authContext.TenantID` from the JWT

## Naming Conventions

### Method Naming by Layer

The same three verbs are used across all layers (repo, service, handler). The verb alone tells you the return shape:

| Verb | Intent | Returns |
|------|--------|---------|
| `Get*` | Single entity | `(*T, error)` |
| `List*` | All items, no pagination | `([]*T, error)` |
| `Find*` | Paginated/filtered search | `(Paginated[T], error)` |

**Repository (port) layer:**
```go
GetByID(ctx, id kernel.UserID, tenantID kernel.TenantID) (*User, error)
GetByEmail(ctx, email string, tenantID kernel.TenantID) (*User, error)
ListByTenant(ctx, tenantID kernel.TenantID) ([]*User, error)
FindByTenant(ctx, tenantID kernel.TenantID, page, size int) (kernel.Paginated[User], error)
```

**Service layer:**
```go
GetUserByID(ctx, userID kernel.UserID, tenantID kernel.TenantID) (*UserResponse, error)
ListTenantUsers(ctx, tenantID kernel.TenantID) (*UserListResponse, error)
FindTenantUsers(ctx, tenantID kernel.TenantID, page, size int) (*kernel.Paginated[UserResponse], error)
```

**Handler layer** — mirrors the service name:
```go
func (h *UserHandlers) GetUser(c *fiber.Ctx) error
func (h *UserHandlers) ListTenantUsers(c *fiber.Ctx) error
func (h *UserHandlers) FindTenantUsers(c *fiber.Ctx) error
```

### Entity ID Fields

Entity struct ID fields use their typed wrapper with `db` and `json` tags preserved:
```go
type User struct {
    ID       kernel.UserID    `db:"id" json:"id"`
    TenantID kernel.TenantID  `db:"tenant_id" json:"tenant_id"`
}
```

## Utility Packages

### asyncx
Concurrency primitives: `Map[K,V]` (concurrent map), `Pool` (worker pool), `Batch` (batch processor), `Debounce`, `Pipeline` (typed stage pipeline), `Stream` (channel-based stream processing).

### fsx
File system abstraction with `FileSystem` interface (Read/Write/Delete/List/Stat). Implementations: `fsxlocal` (local disk), `fsxs3` (AWS S3 with presigned URLs).

### notifx
Email sending abstraction. `EmailSender` interface with implementations: `notifxconsole` (logs to stdout, dev mode), `notifxses` (AWS SES). Supports HTML templates with `TemplateRegistry`.

### jobx
Redis-backed background job queue. Supports delayed jobs, retries with backoff, multiple queues, configurable concurrency. Register handlers by job type, enqueue with `client.Enqueue()`.

### ptrx
Pointer and optional value helpers: `Ptr[T]`, `Val[T]`, `Coalesce`, `Map`, `Filter`, `SlicePtr`, `Zero`. Eliminates boilerplate for working with optional fields.

### logx
Structured logger with levels (Debug/Info/Warn/Error/Fatal), formatters (console with colors, JSON), field chaining (`logx.WithField("key", val).Info("msg")`).

## Adding Domain-Specific Code

When building on manifesto, your domain modules should follow the same structure:

```
internal/
    yourmodule/
        yourmodule.go       # Entities, DTOs, error registry
        port.go             # Repository/service interfaces
        yourmodulesrv/      # Business logic
            service.go
        yourmoduleinfra/    # Postgres/Redis implementations
            postgres.go
        yourmoduleapi/      # Fiber HTTP handlers
            handler.go
```

1. Define your entity and ports in the root package
2. Implement the service in `*srv/`
3. Implement the repository in `*infra/`
4. Create handlers in `*api/`
5. Add a container (or wire directly in `cmd/container.go`)
6. Register routes in `cmd/server.go` between the marker comments
7. Add domain scopes in `internal/iam/scopes/scopes.go`

## Route Registration

Routes are registered in `cmd/server.go` using marker comments:

```go
// manifesto:public-routes    -- add unauthenticated routes here

protected := app.Group("/api/v1", container.IAM.UnifiedAuthMiddleware.Authenticate())

// manifesto:route-registration  -- add authenticated routes here
```

## Configuration & Makefile

All environment variables are defined at the top of the `Makefile` with sensible development defaults. The application reads them via `internal/config/` at startup. **You don't need a `.env` file** -- the Makefile exports everything.

### Environment Variable Groups

| Group | Key Variables | Notes |
|-------|-------------|-------|
| **Server** | `SERVER_PORT` (8080), `ENVIRONMENT` (development), `LOG_LEVEL` (debug), `BASE_URL`, `CORS_ORIGINS` | CORS allows localhost:3000 and :5173 by default |
| **PostgreSQL** | `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_HOST`, `POSTGRES_PORT` | Also aliased as `DB_*` vars for the app |
| **DB Tuning** | `DB_SSL_MODE` (disable), `DB_MAX_OPEN_CONNS` (25), `DB_MAX_IDLE_CONNS` (5), `DB_CONN_MAX_LIFETIME` (5m) | |
| **Redis** | `REDIS_HOST`, `REDIS_PORT` (6379), `REDIS_PASSWORD`, `REDIS_DB` (0) | |
| **JWT** | `JWT_SECRET_KEY`, `JWT_ACCESS_TOKEN_TTL` (15m), `JWT_REFRESH_TOKEN_TTL` (168h), `JWT_ISSUER`, `JWT_AUDIENCE` | Change secret in production |
| **API Keys** | `API_KEY_LIVE_PREFIX`, `API_KEY_TEST_PREFIX`, `API_KEY_TOKEN_LENGTH` (32) | Prefix identifies the key origin |
| **Sessions** | `SESSION_EXPIRATION_TIME` (24h), `SESSION_CLEANUP_INTERVAL` (1h), `SESSION_MAX_PER_USER` (10) | Background cleanup runs automatically |
| **OTP** | `OTP_CODE_LENGTH` (6), `OTP_EXPIRATION_TIME` (10m), `OTP_MAX_ATTEMPTS` (5), `OTP_RATE_LIMIT_WINDOW` (1m) | |
| **Invitations** | `INVITATION_DEFAULT_EXPIRATION_DAYS` (7), `INVITATION_TOKEN_BYTE_LENGTH` (32), `INVITATION_MAX_PENDING_PER_TENANT` (100) | |
| **Password Reset** | `PASSWORD_RESET_EXPIRATION_TIME` (1h), `PASSWORD_RESET_RATE_LIMIT_WINDOW` (15m), `PASSWORD_RESET_MAX_ATTEMPTS` (3) | |
| **Cookies** | `COOKIE_ACCESS_TOKEN_NAME`, `COOKIE_REFRESH_TOKEN_NAME`, `COOKIE_SECURE` (false), `COOKIE_HTTP_ONLY` (true), `COOKIE_SAME_SITE` (Lax) | Set `COOKIE_SECURE=true` in production |
| **OAuth Google** | `OAUTH_GOOGLE_ENABLED`, `OAUTH_GOOGLE_CLIENT_ID`, `OAUTH_GOOGLE_CLIENT_SECRET`, `OAUTH_GOOGLE_REDIRECT_URL` | Disabled by default |
| **OAuth Microsoft** | `OAUTH_MICROSOFT_ENABLED`, `OAUTH_MICROSOFT_CLIENT_ID`, `OAUTH_MICROSOFT_CLIENT_SECRET` | Disabled by default |
| **OAuth State** | `OAUTH_STATE_MANAGER_TYPE` (redis), `OAUTH_STATE_TTL` (10m) | CSRF state stored in Redis |
| **Email** | `EMAIL_PROVIDER` (smtp), `EMAIL_FROM_ADDRESS`, `SMTP_HOST`, `SMTP_PORT`, `SENDGRID_API_KEY`, `AWS_REGION` | Multiple provider options |
| **Storage** | `STORAGE_MODE` (local), `UPLOAD_DIR` (./uploads), `AWS_BUCKET`, `AWS_REGION` | Switch to `s3` for production |
| **Tenant Defaults** | `TENANT_TRIAL_DAYS` (30), `TENANT_MAX_USERS_BASIC` (5), `TENANT_MAX_USERS_PROFESSIONAL` (50), `TENANT_MAX_USERS_ENTERPRISE` (500) | |
| **Bcrypt** | `BCRYPT_COST` (10) | |

### Makefile Commands

**Getting started:**
```bash
make init          # Full project init: tidy deps + start services + migrate + seed
make setup         # Start all services + migrate + seed
make dev           # Run the Go server (go run ./cmd)
make dev-watch     # Run with hot reload (requires 'air')
```

**Docker services (Postgres + Redis via docker-compose):**
```bash
make up            # Start all services
make down          # Stop all services
make down-v        # Stop + remove volumes (fresh start)
make restart       # Restart all
make health        # Check if Postgres and Redis are healthy
make ps            # Show running containers
make logs          # Tail logs for all services
```

**Individual services:**
```bash
make postgres-up / postgres-down / postgres-restart / postgres-logs / postgres-shell
make redis-up / redis-down / redis-restart / redis-logs / redis-cli / redis-shell
make redis-flush   # Flush all Redis data (with confirmation)
```

**Database operations:**
```bash
make migrate       # Run migrations (executes migrations/001_genesis.sql)
make seed          # Run seed file (migrations/seed_test_data.sql)
make psql          # Open psql shell in the container
make conn          # Print the connection string
make db-clean      # Drop all tables (with confirmation)
make db-reset      # Clean + migrate + seed
make db-backup     # Backup to backups/ directory
make db-restore file=backups/backup.sql
make migrate-create name=add_feature_table  # Create timestamped migration file
```

**Build & test:**
```bash
make build         # Build binary to bin/server
make prod          # Build and run
make test          # go test ./...
make test-coverage # Tests with HTML coverage report
make test-race     # Tests with race detector
make lint          # golangci-lint
make fmt           # go fmt
make vet           # go vet
```

**Utilities:**
```bash
make env           # Print all current env var values
make check-deps    # Verify Go, Docker, optional tools are installed
make install-tools # Install air + golangci-lint
make version       # Show Go version and deps
```

### Overriding Variables

Override any variable for a single command:
```bash
make dev SERVER_PORT=9090
make migrate POSTGRES_HOST=remote-db.example.com
```

Or set them in your shell before running make.

## Database

PostgreSQL with `sqlx`. Migrations in `migrations/` folder. The genesis migration creates all IAM tables: tenants, users, invitations, api_keys, refresh_tokens, user_sessions, password_reset_tokens, otps, roles, user_roles.

Users have a `scopes TEXT[]` column (Postgres array). Roles have their own `scopes TEXT[]`. The `user_roles` table is the many-to-many join.

## Key Conventions

- **Typed IDs**: `kernel.UserID`, `kernel.TenantID`, `kernel.RoleID`, `kernel.InvitationID`, `kernel.APIKeyID` are typed strings, not raw strings. Every entity ID must use its typed wrapper. Handlers convert URL params immediately via `kernel.NewXxxID(c.Params("id"))`.
- **Explicit validation**: Request DTOs implement `Validate() error` — no struct tags. Used with `kernel.BindAndValidate[T](c)`.
- **Scope constants**: Always use `iamscopes.ScopeXxx` constants, never hardcoded scope strings.
- **Platform scope protection**: Scopes prefixed `platform:` are automatically restricted to platform operators and hidden from regular tenant admin UIs.
- **Error registries**: Each module has its own `errx.Registry` with prefixed codes.
- **No circular deps**: Modules communicate through interfaces. Cross-module types are in `kernel/`.
- **Explicit over implicit**: No struct tags for DI, no auto-registration, no convention-based routing.
- **Infra is swappable**: All infrastructure is behind interfaces. Switch Postgres for DynamoDB by implementing the port.
- **Method naming**: Same vocabulary across all layers (repo, service, handler):
  - `Get*` — single entity: `GetByID`, `GetByEmail`. Returns `(*T, error)`.
  - `List*` — all items, no pagination: `ListByTenant`, `ListPending`. Returns `([]*T, error)`.
  - `Find*` — paginated/filtered search: `FindByTenant(ctx, tenantID, page, size)`. Returns `(Paginated[T], error)`.

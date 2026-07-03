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
    kernel/              # Shared value objects: UserID, TenantID, AuthContext, Paginated[T]
    errx/                # Structured error system with registries, codes, and HTTP status mapping
    logx/                # Structured logger with levels, formatters (console/JSON)
    asyncx/              # Concurrency primitives: Map, Pool, Batch, Debounce, Pipeline, Stream
    fsx/                 # File system abstraction (local + S3 implementations)
    notifx/              # Email notification abstraction (console + SES implementations)
    jobx/                # Background job queue (Redis-backed, retries, delayed jobs)
    ptrx/                # Pointer/optional helpers: Ptr[T], Val[T], Coalesce, Map, Filter
    iam/                 # Identity & Access Management (see below)
    ai/                  # AI provider abstraction (OpenAI, Anthropic, Bedrock, Gemini)
manifesto.yaml           # Module manifest - tracks which manifesto modules are installed
migrations/              # SQL migrations (Postgres)
```

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
```

Wildcard support:
- `*` grants access to everything (superadmin)
- `users:*` grants all `users:` scopes
- Matching logic is in `kernel.MatchScope()` and `kernel.ScopesContain()`

Scopes are defined in `internal/iam/scopes/`:
- `common_scopes.go` - Reusable scopes that ship with manifesto (users, roles, tenants, api_keys, invitations, settings, audit, reports, integrations, notifications, templates)
- `proj_scopes.go` - **Project-specific scopes go here.** Add your domain scopes to `DomainScopeCategories` and `DomainScopeDescriptions`. They merge with common scopes at init time.

### Roles

Roles (`internal/iam/role/`) are named collections of scopes assigned to users, similar to AWS IAM policies:

```go
type Role struct {
    ID          string
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

Use `middleware.RequireScope("scope:action")` on route groups for authorization.

### API Keys

API keys (`internal/iam/apikey/`) are tenant-scoped, have their own scopes, and can be created by users:
- Keys are hashed (bcrypt) before storage; the raw key is returned only on creation
- Keys have optional expiration
- Keys carry scopes that limit what they can do
- Identified by prefix for easy lookup

### Invitations

The invitation system (`internal/iam/invitation/`) handles tenant user onboarding:
1. Admin creates invitation with email + scopes
2. Email sent via `NotificationService` interface
3. When invited user signs up (OAuth or passwordless), invitation is auto-accepted
4. User receives the scopes from the invitation

### User & Tenant Management

- **Users** (`internal/iam/user/`): CRUD, scope management, status management, pagination
- **Tenants** (`internal/iam/tenant/`): CRUD, subscription plans (Trial/Basic/Professional/Enterprise), status lifecycle, configuration key-value store, user count tracking

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

Add domain scopes in `internal/iam/scopes/proj_scopes.go`.

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

- **Typed IDs**: `kernel.UserID`, `kernel.TenantID` are typed strings, not raw strings
- **Error registries**: Each module has its own `errx.Registry` with prefixed codes
- **No circular deps**: Modules communicate through interfaces. Cross-module types are in `kernel/`
- **Explicit over implicit**: No struct tags for DI, no auto-registration, no convention-based routing
- **Infra is swappable**: All infrastructure is behind interfaces. Switch Postgres for DynamoDB by implementing the port
- **Pagination**: Use `kernel.Paginated[T]` for all list endpoints

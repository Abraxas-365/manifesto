# Architecture Manifesto: Building Scalable Multi-Tenant Systems

## Philosophy & Core Principles

This document outlines the architectural decisions, patterns, and principles that guide this project. These are not just preferences—they represent hard-won lessons about building maintainable, scalable, and type-safe enterprise systems.

---

## 1. **Domain-Driven Design (DDD) as Foundation**

### Why DDD?

The business domain is **complex**, and code should **mirror that complexity explicitly** rather than hide it behind generic CRUD operations.

### Our Implementation:

* **Rich Domain Entities** with behavior, not anemic data structures
* **Value Objects** for type safety (`kernel.Email`, `kernel.DNI`, `kernel.JobID`)
* **Domain Methods** that encapsulate business rules (`Tenant.CanAddUser()`, `User.HasScope()`)
* **Repository Interfaces** that speak the domain language

```go
// ✅ GOOD: Rich entity with domain logic
func (t *Tenant) CanAddUser() bool {
    if !t.IsActive() { return false }
    if t.IsTrialExpired() || t.IsSubscriptionExpired() { return false }
    return t.CurrentUsers < t.MaxUsers
}

// ❌ BAD: Anemic entity
type Tenant struct {
    ID string
    CurrentUsers int
    MaxUsers int
}
```

---

## 2. **Layered Architecture: Clear Separation of Concerns**

### The Layers:

```
┌─────────────────────────────────────┐
│   API Layer (handlers, DTOs)        │  ← HTTP/Fiber handlers
├─────────────────────────────────────┤
│   Service Layer (business logic)    │  ← Orchestration & workflows
├─────────────────────────────────────┤
│   Domain Layer (entities, rules)    │  ← Core business logic
├─────────────────────────────────────┤
│   Repository Layer (persistence)    │  ← Data access contracts
├─────────────────────────────────────┤
│   Infrastructure (DB, S3, etc)      │  ← Implementation details
└─────────────────────────────────────┘
```

### Rules:

1. **Dependencies flow downward only** (no cyclic dependencies)
2. **Domain layer has NO external dependencies** (pure Go)
3. **Repository interfaces live in domain**, implementations in infrastructure
4. **Services orchestrate**, entities enforce rules

---

## 3. **Type Safety Through Value Objects**

### The `pkg/kernel` Package

Instead of passing `string` everywhere, we use **strongly-typed domain primitives**:

```go
type UserID string
type TenantID string
type CandidateID string
type Email string
type DNI struct {
    Type   DNIType
    Number string
}
```

### Benefits:

* **Compile-time safety** - Can't accidentally pass a `UserID` where `TenantID` is expected
* **Self-documenting code** - `func GetUser(id kernel.UserID)` is clearer than `func GetUser(id string)`
* **Validation in one place** - `DNI.IsValid()` encapsulates all validation logic
* **Easy refactoring** - Change the underlying type without touching all usages

---

## 4. **Repository Pattern: Abstracting Data Access**

### Why Repositories?

* **Testability** - Mock repositories in tests
* **Flexibility** - Swap PostgreSQL for MongoDB without changing business logic
* **Domain language** - `FindByEmail()` not `SELECT * FROM users WHERE...`

### Our Convention:

```go
// Domain layer defines the CONTRACT
type Repository interface {
    Create(ctx context.Context, candidate *Candidate) error
    GetByID(ctx context.Context, id kernel.CandidateID) (*Candidate, error)
    GetByEmail(ctx context.Context, email kernel.Email) (*Candidate, error)
    Search(ctx context.Context, req SearchCandidatesRequest) (*kernel.Paginated[Candidate], error)
}

// Infrastructure layer provides IMPLEMENTATION
type PostgresCandidateRepository struct {
    db *sqlx.DB
}
```

**Never leak infrastructure details** (SQL, Mongo queries) into domain/service layers.

---

## 5. **Service Layer: Orchestration & Coordination**

### Service Responsibilities:

* **Coordinate multiple repositories**
* **Enforce cross-entity business rules**
* **Handle transactions**
* **Convert between DTOs and domain entities**

### Example Pattern:

```go
func (s *UserService) CreateUser(ctx context.Context, req user.CreateUserRequest, creatorID kernel.UserID) (*user.User, error) {
    // 1. Validate dependencies
    tenantEntity, err := s.tenantRepo.FindByID(ctx, req.TenantID)
    if err != nil { return nil, tenant.ErrTenantNotFound() }
    
    // 2. Business rule validation
    if !tenantEntity.CanAddUser() {
        return nil, tenant.ErrMaxUsersReached()
    }
    
    // 3. Create domain entity
    newUser := &user.User{...}
    
    // 4. Persist
    if err := s.userRepo.Save(ctx, *newUser); err != nil {
        return nil, errx.Wrap(err, "failed to save user", errx.TypeInternal)
    }
    
    // 5. Update related entities
    tenantEntity.AddUser()
    s.tenantRepo.Save(ctx, *tenantEntity)
    
    return newUser, nil
}
```

---

## 6. **DTOs: Input/Output Transformation**

### Why DTOs?

* **API versioning** - Change DTOs without changing domain entities
* **Security** - Don't expose internal IDs or sensitive fields
* **Validation at boundaries** - Validate input before entering domain
* **Separation** - Domain entities ≠ API responses

### Our Pattern:

```go
// Input DTO
type CreateCandidateRequest struct {
    Email     kernel.Email     `json:"email" validate:"required,email"`
    FirstName kernel.FirstName `json:"first_name" validate:"required"`
    DNI       kernel.DNI       `json:"dni" validate:"required"`
}

// Output DTO
type CandidateResponse struct {
    ID        kernel.CandidateID `json:"id"`
    Email     kernel.Email       `json:"email"`
    FirstName kernel.FirstName   `json:"first_name"`
}

// Domain Entity (different from DTOs!)
type Candidate struct {
    ID              kernel.CandidateID
    Email           kernel.Email
    FirstName       kernel.FirstName
    CreatedAt       time.Time  // Not exposed in DTO
    PasswordHash    string     // Never exposed
}
```

---

## 7. **Error Handling: Rich, Structured Errors**

### The `pkg/errx` Package

We reject generic `error` in favor of **rich error types** with context:

```go
// Error Registry per module
var ErrRegistry = errx.NewRegistry("TENANT")

var CodeMaxUsersReached = ErrRegistry.Register(
    "MAX_USERS_REACHED", 
    errx.TypeBusiness, 
    http.StatusForbidden, 
    "Maximum users reached"
)

// Usage
return ErrMaxUsersReached().
    WithDetail("max_users", t.MaxUsers).
    WithDetail("current_users", t.CurrentUsers)
```

### Benefits:

* **Typed errors** - `errx.Type` categorizes errors (Validation, Business, Internal)
* **HTTP status codes** - Automatic mapping to correct HTTP responses
* **Structured context** - `WithDetail()` adds debugging information
* **Error codes** - Machine-readable error identifiers
* **Wrapping** - Preserve error chains with `errx.Wrap()`

---

## 8. **Multi-Tenancy: First-Class Concern**

### Tenant Isolation Strategy:

* **Every entity** has a `TenantID` 
* **All queries** filter by tenant
* **AuthContext** carries `TenantID` through the request lifecycle
* **Repositories** enforce tenant boundaries

```go
// ✅ Always scoped to tenant
func (r *Repository) FindByID(ctx context.Context, id UserID, tenantID TenantID) (*User, error)

// ❌ Never global lookups
func (r *Repository) FindByID(ctx context.Context, id UserID) (*User, error)
```

### Tenant Context Propagation:

```go
type AuthContext struct {
    UserID   *UserID
    TenantID TenantID  // ← Always present
    Scopes   []string
}

// Injected via middleware, available everywhere
authContext, _ := auth.GetAuthContext(c)
```

---

## 9. **Scope-Based Permissions: Fine-Grained Access Control**

### Why Scopes Instead of Roles?

* **Composability** - Mix and match permissions
* **API-friendly** - Works for both users and API keys
* **OAuth-compatible** - Standard pattern
* **Wildcard support** - `jobs:*` matches all job permissions

### Our Implementation:

```go
// Defined in auth package
const (
    ScopeJobsRead    = "jobs:read"
    ScopeJobsWrite   = "jobs:write"
    ScopeCandidatesAll = "candidates:*"
)

// Middleware enforcement
func (am *UnifiedAuthMiddleware) RequireScope(scope string) fiber.Handler {
    return func(c *fiber.Ctx) error {
        authContext, ok := GetAuthContext(c)
        if !authContext.HasScope(scope) {
            return c.Status(fiber.StatusForbidden).JSON(...)
        }
        return c.Next()
    }
}
```

### Scope Organization:

* **Common scopes** in `scopes_common.go` (reusable across projects)
* **Domain scopes** in `scopes_domain.go` (ATS-specific)
* **Scope groups** for role templates (`recruiter`, `hiring_manager`)

---

## 10. **Authentication: OAuth + JWT + API Keys**

### Unified Auth Strategy:

```go
// Single middleware handles both
func (am *UnifiedAuthMiddleware) Authenticate() fiber.Handler {
    return func(c *fiber.Ctx) error {
        apiKey := extractAPIKey(c)
        if apiKey != "" {
            return am.authenticateAPIKey(c, apiKey)  // ← API Key auth
        }
        return am.authenticateJWT(c)  // ← JWT auth
    }
}
```

### OAuth Flow:

1. **Invitation required** - No self-signup (B2B SaaS model)
2. **State management** - CSRF protection via state tokens
3. **Provider abstraction** - Google, Microsoft behind `OAuthService` interface
4. **Token generation** - Internal JWTs after OAuth success

---

## 11. **Reusable Packages: Build Once, Use Everywhere**

### `pkg/errx` - Error Handling

* Type-safe error creation
* HTTP status mapping
* Error registries per module
* Structured error details

### `pkg/logx` - Logging

* Rust-inspired colored console output
* JSON/CloudWatch formatters
* Structured logging with fields
* Environment-based configuration

### `pkg/fsx` - File System Abstraction

* Interface-based (works with S3, local FS, etc.)
* Context-aware operations
* Consistent error handling via `errx`

### `pkg/ptrx` - Pointer Utilities

* AWS SDK-style pointer helpers
* Generic `Value[T]` and `ValueOr[T]`
* Type-safe nullable fields

### `pkg/kernel` - Domain Primitives

* Shared value objects (`UserID`, `TenantID`, etc.)
* `AuthContext` for request context
* `Paginated[T]` for consistent pagination
* No business logic (just types)

---

## 12. **Pagination: Consistent & Type-Safe**

### The Pattern:

```go
type Paginated[T any] struct {
    Items []T  `json:"items"`
    Page  Page `json:"pagination"`
    Empty bool `json:"empty"`
}

// Usage
func List(ctx context.Context, opts kernel.PaginationOptions) (*kernel.Paginated[Candidate], error)
```

### Benefits:

* **Generic** - Works with any entity type
* **Metadata included** - Total count, page numbers, etc.
* **Helper methods** - `HasNext()`, `HasPrevious()`

---

## 13. **Dependency Injection: Explicit & Testable**

### Constructor Injection:

```go
type UserService struct {
    userRepo     user.UserRepository
    tenantRepo   tenant.TenantRepository
    roleRepo     role.RoleRepository
    passwordSvc  user.PasswordService
}

func NewUserService(
    userRepo user.UserRepository,
    tenantRepo tenant.TenantRepository,
    roleRepo role.RoleRepository,
    passwordSvc user.PasswordService,
) *UserService {
    return &UserService{
        userRepo:    userRepo,
        tenantRepo:  tenantRepo,
        roleRepo:    roleRepo,
        passwordSvc: passwordSvc,
    }
}
```

### No Magic:

* **No reflection-based DI** (looking at you, Spring)
* **No service locators**
* **Explicit wiring** in `main.go` or DI container
* **Easy to test** - Just pass mocks

---

## 14. **Package Organization: Domain-Centric**

### Structure:

```
pkg/
├── kernel/           # Shared domain primitives
├── errx/             # Error handling framework
├── logx/             # Logging framework
├── fsx/              # File system abstraction
├── ptrx/             # Pointer utilities
├── iam/              # Identity & Access Management domain
│   ├── user/         # User entity + repository interface
│   ├── tenant/       # Tenant entity + repository interface
│   ├── role/         # Role entity + repository interface
│   ├── invitation/   # Invitation entity + repository interface
│   ├── apikey/       # API Key entity + repository interface
│   └── auth/         # Authentication logic
│       ├── handlers.go
│       ├── middleware.go
│       ├── jwt.go
│       ├── oauth_google.go
│       └── scopes.go
├── candidate/        # Candidate domain
├── job/              # Job domain
└── application/      # Application domain
```

### Principles:

* **Domain packages are independent** (candidate doesn't import job)
* **Shared types in kernel** (not in individual domains)
* **No circular dependencies** (enforced by Go)
* **Each domain owns its errors** (`user.ErrUserNotFound()`)

---

## 15. **Middleware: Composable Security Layers**

### Unified Auth Middleware:

```go
// Supports both JWT and API Keys
app.Use(authMiddleware.Authenticate())

// Require specific scopes
app.Post("/jobs", 
    authMiddleware.RequireScope(auth.ScopeJobsWrite),
    jobHandlers.CreateJob,
)

// Require admin OR specific scope
app.Delete("/users/:id",
    authMiddleware.RequireAdminOrScope(auth.ScopeUsersDelete),
    userHandlers.DeleteUser,
)

// Require ALL scopes (AND logic)
app.Post("/sensitive",
    authMiddleware.RequireAllScopes(
        auth.ScopeJobsWrite,
        auth.ScopeCandidatesWrite,
    ),
    handlers.SensitiveOperation,
)
```

---

## 16. **Configuration: Environment-Driven**

### Pattern:

```go
// 1. Define config struct
type Config struct {
    JWT   JWTConfig
    OAuth OAuthConfigs
}

// 2. Provide defaults
func DefaultConfig() Config { ... }

// 3. Load from environment
func LoadFromEnv() *Config {
    config := DefaultConfig()
    if level := os.Getenv("LOG_LEVEL"); level != "" {
        config.Level = ParseLevel(level)
    }
    return config
}

// 4. Validate on startup
if err := config.Validate(); err != nil {
    log.Fatal(err)
}
```

**Fail fast** - Invalid configuration = app won't start.

---

## 17. **Error Handling Philosophy**

### Principles:

1. **Errors are data** - Structure them properly
2. **Context matters** - Use `WithDetail()` liberally
3. **Type errors** - `TypeValidation` vs `TypeBusiness` vs `TypeInternal`
4. **Wrap, don't hide** - Preserve error chains
5. **HTTP-aware** - Errors know their HTTP status codes

### Example:

```go
// ✅ Rich error with context
return s3Errors.NewWithCause(ErrFailedUpload, err).
    WithDetail("path", path).
    WithDetail("bucket", fs.bucket).
    WithDetail("key", key)

// ❌ Generic error
return fmt.Errorf("upload failed: %w", err)
```

---

## 18. **Testing Strategy**

### What We Test:

1. **Domain logic** - Unit tests for entities
2. **Service layer** - Integration tests with mock repos
3. **API handlers** - E2E tests with test database
4. **Validation** - Edge cases for value objects

### Repository Mocks:

```go
type MockUserRepository struct {
    users map[kernel.UserID]*user.User
}

func (m *MockUserRepository) FindByID(ctx context.Context, id kernel.UserID, tenantID kernel.TenantID) (*user.User, error) {
    if u, ok := m.users[id]; ok && u.TenantID == tenantID {
        return u, nil
    }
    return nil, user.ErrUserNotFound()
}
```

---

## 19. **Context Propagation: Request Lifecycle**

### The `AuthContext`:

```go
type AuthContext struct {
    UserID   *UserID
    TenantID TenantID  // ← Always present
    Email    string
    Scopes   []string
    IsAPIKey bool
}

// Set by middleware
func (am *UnifiedAuthMiddleware) Authenticate() fiber.Handler {
    return func(c *fiber.Ctx) error {
        // ... validate token/API key ...
        c.Locals("auth", authContext)
        return c.Next()
    }
}

// Retrieved in handlers
func (h *Handlers) CreateJob(c *fiber.Ctx) error {
    authContext, _ := auth.GetAuthContext(c)
    // Use authContext.TenantID for tenant-scoped operations
}
```

---

## 20. **Security Principles**

### Defense in Depth:

1. **Middleware authentication** - Validate before reaching handlers
2. **Scope enforcement** - Fine-grained permissions
3. **Tenant isolation** - Every query filtered by `TenantID`
4. **Input validation** - DTOs with `validate` tags
5. **API key hashing** - Never store plaintext secrets
6. **Token expiration** - Short-lived JWTs (15 min), refresh tokens (7 days)
7. **OAuth state** - CSRF protection

### Invitation-Only Registration:

```go
// NO self-signup for B2B SaaS
if invitationToken == "" {
    return errx.New("invitation required for registration", errx.TypeAuthorization)
}
```

---

## 21. **Observability: Logging Best Practices**

### Structured Logging:

```go
// ✅ Good - Structured with context
logx.WithFields(logx.Fields{
    "user_id":   userID,
    "tenant_id": tenantID,
    "operation": "create_user",
}).Info("User created successfully")

// ❌ Bad - Unstructured string interpolation
log.Printf("User %s created for tenant %s", userID, tenantID)
```

### Log Levels:

* **TRACE** - Function entry/exit (development only)
* **DEBUG** - Variable values, flow control
* **INFO** - Business events (user created, invoice sent)
* **WARN** - Recoverable errors (retry succeeded)
* **ERROR** - Unrecoverable errors
* **FATAL** - App shutdown events

---

## 22. **Database Strategy**

### Migration Philosophy:

* **Version controlled** - Migrations in `/migrations`
* **Idempotent** - Can run multiple times safely
* **Rollback support** - Down migrations always provided
* **Data migrations separate** from schema migrations

### Query Patterns:

* **Prepared statements** - Prevent SQL injection
* **Batch operations** - Bulk inserts/updates when possible
* **Indexes** - On foreign keys and frequently queried columns
* **Soft deletes** - `deleted_at` timestamp for audit trails

---

## 23. **API Design Principles**

### RESTful Conventions:

```
POST   /api/jobs              ← Create
GET    /api/jobs              ← List
GET    /api/jobs/:id          ← Get one
PUT    /api/jobs/:id          ← Update
DELETE /api/jobs/:id          ← Delete

POST   /api/jobs/:id/publish  ← Actions as sub-resources
```

### Response Format:

```json
{
  "items": [...],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 156,
    "pages": 8
  },
  "empty": false
}
```

---

## 24. **Code Style & Conventions**

### Naming:

* **Entities** - Singular nouns (`User`, `Tenant`, `Job`)
* **Repositories** - `EntityRepository` (`UserRepository`)
* **Services** - `EntityService` (`UserService`, `TenantService`)
* **Handlers** - `EntityHandlers` (`JobHandlers`)
* **DTOs** - Suffixed with purpose (`CreateUserRequest`, `UserResponse`)

### File Organization:

```
user/
├── user.go          # Entity + domain methods + DTOs
├── repository.go    # Repository interface
├── errors.go        # Error registry (if complex enough)
└── usersrv/         # Service layer (separate package to avoid cycles)
    └── service.go
```

---

## 25. **What We Avoid**

### Anti-Patterns We Reject:

* ❌ **God objects** - No single struct that does everything
* ❌ **Anemic domain models** - Entities have behavior, not just getters/setters
* ❌ **Repository sprawl** - One repository per aggregate root
* ❌ **Service layer bypass** - Never call repos directly from handlers
* ❌ **DTO reuse** - Don't use same DTO for input and output
* ❌ **Null pointer exceptions** - Use pointer helper package
* ❌ **Primitive obsession** - Use value objects, not `string` everywhere
* ❌ **Magic strings** - Constants for error codes, scopes, etc.

---

## 26. **Performance Considerations**

### Optimization Strategy:

* **Eager loading** - Use `GetWithDetails()` to avoid N+1 queries
* **Pagination** - Never return unbounded lists
* **Caching** - At service layer for expensive operations
* **Connection pooling** - Database connections
* **Goroutines for async** - Non-blocking operations (email sending, etc.)

### Example:

```go
// ✅ Single query with JOIN
GetWithDetails(ctx, id) (*ApplicationWithDetailsResponse, error)

// ❌ N+1 queries
applications := repo.List(ctx)
for _, app := range applications {
    candidate := candidateRepo.GetByID(app.CandidateID)  // ← N queries!
    job := jobRepo.GetByID(app.JobID)
}
```

---

## 27. **Documentation Standards**

### Code Comments:

```go
// Service comments explain WHAT and WHY
// CreateUser creates a new user in the system.
// It validates tenant capacity, checks for duplicate emails,
// and assigns default scopes for the tenant.
func (s *UserService) CreateUser(...)

// Domain methods document business rules
// CanAddUser verifies if the tenant can add more users
// by checking active status, subscription limits, and quotas.
func (t *Tenant) CanAddUser() bool
```

### Self-Documenting Code:

* **Method names** should be unambiguous
* **Variable names** should be descriptive
* **Type names** should convey purpose
* **Comments** explain non-obvious business rules

---

## Conclusion: Architecture as Product

This architecture is not accidental. Every decision serves **specific goals**:

* ✅ **Maintainability** - New developers can navigate the codebase
* ✅ **Testability** - Mock interfaces, not implementations
* ✅ **Scalability** - Multi-tenant from day one
* ✅ **Security** - Defense in depth, scope-based permissions
* ✅ **Type safety** - Catch errors at compile time
* ✅ **Flexibility** - Swap implementations without changing contracts
* ✅ **Observability** - Rich errors and structured logging

**Good architecture makes the right thing easy and the wrong thing hard.**

This is that architecture.

---

*Version: 1.0*  
*Last Updated: 2025-11-24*

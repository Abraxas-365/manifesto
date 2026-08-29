# Architecture Manifesto: Building Scalable Multi-Tenant Systems

## 🚀 Using This as a Project Skeleton

This repository serves as a **production-ready Go project skeleton** with built-in patterns for DDD, multi-tenancy, authentication, and scalable architecture.

### Quick Start

Use the **[Manifesto CLI](https://github.com/Abraxas-365/manifesto-cli)** to scaffold a new project in seconds:

```bash
go install github.com/Abraxas-365/manifesto-cli/cmd/manifesto@latest
```

Requires Go 1.23+.

**1. Create a new project:**

```bash
# Interactive — core modules only
manifesto init myapp --module github.com/me/myapp

# With optional modules
manifesto init myapp --module github.com/me/myapp --with iam,fsx

# Everything included
manifesto init myapp --module github.com/me/myapp --all
```

**2. Add domain packages:**

```bash
cd myapp
manifesto add internal/recruitment/candidate
```

**3. Verify the setup:**

```bash
go mod tidy
go build ./...
```

> For a full list of CLI commands and available modules, see the [Manifesto CLI repository](https://github.com/Abraxas-365/manifesto-cli).

---

## Philosophy & Core Principles

This document outlines the architectural decisions, patterns, and principles that guide this project. These are not just preferences — they represent hard-won lessons about building maintainable, scalable, and type-safe enterprise systems.

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
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃   API Layer (handlers, DTOs)        ┃  ← HTTP/Fiber handlers
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Service Layer (business logic)    ┃  ← Orchestration & workflows
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Domain Layer (entities, rules)    ┃  ← Core business logic
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Repository Layer (persistence)    ┃  ← Data access contracts
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Infrastructure (DB, S3, etc)      ┃  ← Implementation details
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

### Rules:

1. **Dependencies flow downward only** (no cyclic dependencies)
2. **Domain layer has NO external dependencies** (pure Go)
3. **Repository interfaces live in domain**, implementations in infrastructure
4. **Services orchestrate**, entities enforce rules

---

## 3. **Type Safety Through Value Objects**

### The `internal/kernel` Package

Instead of passing `string` everywhere, we use **strongly-typed domain primitives**:

```go
type UserID string
type TenantID string
type RoleID string
type InvitationID string
type APIKeyID string
```

Each typed ID has `NewXxxID(string)`, `.String()`, and `.IsEmpty()` methods. Handlers convert URL params immediately:

```go
userID := kernel.NewUserID(c.Params("id"))
```

Raw `string` must never flow through service or domain layers for entity IDs.

### Benefits:

* **Compile-time safety** — Can't accidentally pass a `UserID` where `TenantID` is expected
* **Self-documenting code** — `func GetUser(id kernel.UserID)` is clearer than `func GetUser(id string)`
* **Validation in one place** — `DNI.IsValid()` encapsulates all validation logic
* **Easy refactoring** — Change the underlying type without touching all usages

---

## 4. **Repository Pattern: Abstracting Data Access**

### Why Repositories?

* **Testability** — Mock repositories in tests
* **Flexibility** — Swap PostgreSQL for MongoDB without changing business logic
* **Domain language** — `GetByEmail()` not `SELECT * FROM users WHERE...`

### Method Naming Convention

The same three verbs are used across **all layers** (repo, service, handler). The verb alone tells you the return shape:

| Verb | Intent | Returns |
|:---|:---|:---|
| `Get*` | Single entity | `(*T, error)` |
| `List*` | All items, no pagination | `([]*T, error)` |
| `Find*` | Paginated/filtered search | `(Paginated[T], error)` |

```go
// Repository port
type Repository interface {
    GetByID(ctx context.Context, id kernel.CandidateID) (*Candidate, error)
    GetByEmail(ctx context.Context, email string) (*Candidate, error)
    ListByTenant(ctx context.Context, tenantID kernel.TenantID) ([]*Candidate, error)
    Find(ctx context.Context, req SearchCandidatesRequest) (*kernel.Paginated[Candidate], error)
}

// Service mirrors the same verbs
func (s *CandidateService) GetCandidate(ctx, id) (*CandidateResponse, error)
func (s *CandidateService) ListTenantCandidates(ctx, tenantID) ([]*CandidateResponse, error)
func (s *CandidateService) FindCandidates(ctx, req) (*Paginated[CandidateResponse], error)
```

**Never leak infrastructure details** (SQL, Mongo queries) into domain/service layers.

---

## 5. **Domain Independence: The Cross-Domain Relationship Pattern**

### The Problem: Avoiding Tight Coupling

**❌ Wrong Approach:**

```go
// recruitment/candidate/candidate.go
package candidate

import "yourapp/recruitment/job"  // ❌ Creates tight coupling!

func (c *Candidate) GetAppliedJobs() ([]job.Job, error) {
    // This violates domain independence
}
```

### The Solution: Bridge Domain + Service Orchestration

When domains need to reference each other (e.g., candidates applying to jobs), we use a **three-tier strategy**:

#### Tier 1: Bridge Domain (Application Domain)

Create a separate domain that represents the **relationship** between entities:

```go
// recruitment/application/application.go
package application

import (
    "yourapp/internal/kernel"
    "time"
)

// Application is the aggregate root for candidate-job relationships
// ✅ It only references IDs from kernel, not full entities
type Application struct {
    ID          kernel.ApplicationID
    CandidateID kernel.CandidateID  // Reference by ID only
    JobID       kernel.JobID        // Reference by ID only
    TenantID    kernel.TenantID
    Status      ApplicationStatus
    AppliedAt   time.Time
    UpdatedAt   time.Time
}

// Domain methods on the relationship
func (a *Application) CanWithdraw() bool {
    return a.Status == StatusPending || a.Status == StatusReviewed
}

func (a *Application) Withdraw() error {
    if !a.CanWithdraw() {
        return ErrCannotWithdrawApplication()
    }
    a.Status = StatusWithdrawn
    a.UpdatedAt = time.Now()
    return nil
}
```

#### Tier 2: Repository Interface for Relationship Queries

```go
// recruitment/application/repository.go
package application

type Repository interface {
    Create(ctx context.Context, app *Application) error
    Update(ctx context.Context, app *Application) error
    GetByID(ctx context.Context, id kernel.ApplicationID) (*Application, error)
    ListByCandidateID(ctx context.Context, candidateID kernel.CandidateID, opts kernel.PaginationOptions) (*kernel.Paginated[Application], error)
    ListByJobID(ctx context.Context, jobID kernel.JobID, opts kernel.PaginationOptions) (*kernel.Paginated[Application], error)
    ExistsByCandidateAndJob(ctx context.Context, candidateID kernel.CandidateID, jobID kernel.JobID) (bool, error)
    GetByIDs(ctx context.Context, ids []kernel.ApplicationID) ([]*Application, error)
    CountByJob(ctx context.Context, jobID kernel.JobID) (int, error)
}
```

#### Tier 3: Service Layer Orchestrates Cross-Domain Logic

```go
// recruitment/application/applicationsrv/service.go
package applicationsrv

type ApplicationService struct {
    appRepo       application.Repository
    candidateRepo candidate.Repository
    jobRepo       job.Repository
    // ✅ No UnitOfWork here — simple reads/single-repo writes don't need it
}

func NewApplicationService(
    appRepo application.Repository,
    candidateRepo candidate.Repository,
    jobRepo job.Repository,
) *ApplicationService {
    return &ApplicationService{
        appRepo:       appRepo,
        candidateRepo: candidateRepo,
        jobRepo:       jobRepo,
    }
}

// GetCandidateApplications — read-only cross-domain query, no transaction needed
func (s *ApplicationService) GetCandidateApplications(
    ctx context.Context,
    candidateID kernel.CandidateID,
    opts kernel.PaginationOptions,
) (*kernel.Paginated[application.ApplicationWithDetails], error) {
    apps, err := s.appRepo.ListByCandidateID(ctx, candidateID, opts)
    if err != nil {
        return nil, errx.Wrap(err, "failed to list applications", errx.TypeInternal)
    }

    if apps.Empty {
        return &kernel.Paginated[application.ApplicationWithDetails]{Empty: true}, nil
    }

    jobIDs := make([]kernel.JobID, len(apps.Items))
    for i, app := range apps.Items {
        jobIDs[i] = app.JobID
    }

    jobs, err := s.jobRepo.GetByIDs(ctx, jobIDs)
    if err != nil {
        return nil, errx.Wrap(err, "failed to fetch jobs", errx.TypeInternal)
    }

    jobMap := make(map[kernel.JobID]*job.Job)
    for _, j := range jobs {
        jobMap[j.ID] = j
    }

    result := make([]application.ApplicationWithDetails, len(apps.Items))
    for i, app := range apps.Items {
        j := jobMap[app.JobID]
        result[i] = application.ApplicationWithDetails{
            ID:          app.ID,
            Status:      app.Status,
            AppliedAt:   app.AppliedAt,
            JobID:       app.JobID,
            JobTitle:    j.Title,
            CompanyName: j.CompanyName,
            Location:    j.Location,
        }
    }

    return &kernel.Paginated[application.ApplicationWithDetails]{
        Items: result,
        Page:  apps.Page,
    }, nil
}

// WithdrawApplication — single repo write, no transaction needed
func (s *ApplicationService) WithdrawApplication(
    ctx context.Context,
    applicationID kernel.ApplicationID,
    candidateID kernel.CandidateID,
) error {
    app, err := s.appRepo.GetByID(ctx, applicationID)
    if err != nil {
        return application.ErrApplicationNotFound()
    }

    if app.CandidateID != candidateID {
        return application.ErrUnauthorizedAccess()
    }

    if err := app.Withdraw(); err != nil {
        return err
    }

    return s.appRepo.Update(ctx, app)
}
```

### Domain Independence Rules:

| ✅ Allowed | ❌ Forbidden |
|:---|:---|
| Domain imports `kernel` (shared types) | Domain imports another domain |
| Service imports multiple domains | Repository imports domain |
| Application domain references IDs | Application domain embeds entities |
| DTOs combine cross-domain data | Entities have cross-domain dependencies |
| Service orchestrates cross-domain logic | Handler directly calls multiple repos |

### Dependency Flow Diagram:

```
┌─────────────────────────────────────────────────┐
│   Service Layer (applicationsrv package)        │
│  ✅ Can import: application, candidate, job     │
└────────────────────┬────────────────────────────┘
                     │ Orchestrates
         ┌───────────┼───────────┐
         │           │           │
         ▼           ▼           ▼
┌────────────┐ ┌────────────┐ ┌────────────┐
│ application│ │  candidate │ │    job     │
│   domain   │ │   domain   │ │   domain   │
└─────┬──────┘ └─────┬──────┘ └─────┬──────┘
      │              │              │
      └──────────────┴──────────────┘
                     │
                     ▼
              ┌────────────┐
              │   kernel   │ ← Shared primitives
              │  (no deps) │
              └────────────┘
```

---

## 6. **Service Layer: Orchestration & Coordination**

### Service Responsibilities:

* **Coordinate multiple repositories**
* **Enforce cross-entity business rules**
* **Handle transactions when necessary** (see section 7)
* **Convert between DTOs and domain entities**
* **Bridge multiple domains**

### Example Pattern:

```go
// internal/iam/user/usersrv/service.go
package usersrv

// Simple service with no UoW — only needed when the service
// has operations that write to multiple repositories atomically.
type CandidateService struct {
    candidateRepo candidate.Repository
}

func NewCandidateService(candidateRepo candidate.Repository) *CandidateService {
    return &CandidateService{candidateRepo: candidateRepo}
}

// ✅ Single-repo read — no transaction, no UoW
func (s *CandidateService) GetCandidate(ctx context.Context, id kernel.CandidateID) (*candidate.Candidate, error) {
    return s.candidateRepo.GetByID(ctx, id)
}

// ✅ Single-repo write — no transaction, no UoW
func (s *CandidateService) DeactivateCandidate(ctx context.Context, id kernel.CandidateID) error {
    c, err := s.candidateRepo.GetByID(ctx, id)
    if err != nil {
        return candidate.ErrCandidateNotFound()
    }
    c.Deactivate()
    return s.candidateRepo.Update(ctx, c)
}
```

---

## 7. **Transactions: Unit of Work Pattern (When Needed)**

### ⚠️ Not Every Service Needs This

The Unit of Work (UoW) pattern exists to solve one specific problem: **ensuring atomicity across multiple repository writes**. Most simple services — those that only read data or only write to a single repository — **do not need a UoW**. Adding it everywhere introduces unnecessary complexity.

> **Rule of thumb:** Only inject `kernel.UnitOfWork` into a service if that service has at least one operation that **writes to two or more repositories** and must succeed or fail as a unit.

### The Problem: Multi-Repository Operations

```go
// ❌ PROBLEM: What if step 2 fails? Step 1 is already committed!
func (s *UserService) CreateUser(ctx context.Context, req CreateUserRequest) error {
    userRepo.Create(ctx, user)      // Step 1 ✅
    tenantRepo.UpdateCount(ctx, t)  // Step 2 ❌ FAILS — user was already saved!
    roleRepo.Assign(ctx, role)      // Step 3 never runs
}
```

### The Solution: Unit of Work Interface

```go
// internal/kernel/uow.go
package kernel

import "context"

// UnitOfWork coordinates transactions across multiple repositories.
// Only use this in services that require atomic multi-repo writes.
type UnitOfWork interface {
    Begin(ctx context.Context) (context.Context, error)
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}

// WithTransaction executes fn within a transaction
func WithTransaction(ctx context.Context, uow UnitOfWork, fn func(context.Context) error) error {
    txCtx, err := uow.Begin(ctx)
    if err != nil {
        return err
    }

    defer func() {
        if r := recover(); r != nil {
            uow.Rollback(txCtx)
            panic(r)
        }
    }()

    if err := fn(txCtx); err != nil {
        uow.Rollback(txCtx)
        return err
    }

    return uow.Commit(txCtx)
}
```

### Infrastructure Implementation

```go
// internal/iam/iaminfra/uow.go
type PostgresUnitOfWork struct {
    db *sqlx.DB
}

func NewPostgresUnitOfWork(db *sqlx.DB) kernel.UnitOfWork {
    return &PostgresUnitOfWork{db: db}
}

func (uow *PostgresUnitOfWork) Begin(ctx context.Context) (context.Context, error) {
    tx, err := uow.db.BeginTxx(ctx, nil)
    if err != nil {
        return ctx, err
    }
    return context.WithValue(ctx, "db_tx", tx), nil
}

func (uow *PostgresUnitOfWork) Commit(ctx context.Context) error {
    if tx := uow.getTx(ctx); tx != nil {
        return tx.Commit()
    }
    return nil
}

func (uow *PostgresUnitOfWork) Rollback(ctx context.Context) error {
    if tx := uow.getTx(ctx); tx != nil {
        return tx.Rollback()
    }
    return nil
}

func (uow *PostgresUnitOfWork) getTx(ctx context.Context) *sqlx.Tx {
    if tx, ok := ctx.Value("db_tx").(*sqlx.Tx); ok {
        return tx
    }
    return nil
}
```

### Repository Support for Transactions

Any repository that may participate in a transaction must support both transactional and non-transactional contexts via a `getExecutor` helper:

```go
// ✅ THE MAGIC: Use transaction if present, otherwise use DB directly
func (r *PostgresUserRepository) getExecutor(ctx context.Context) sqlx.ExtContext {
    if tx, ok := ctx.Value("db_tx").(*sqlx.Tx); ok {
        return tx
    }
    return r.db
}

func (r *PostgresUserRepository) Create(ctx context.Context, u *user.User) error {
    executor := r.getExecutor(ctx)
    query := `INSERT INTO users (id, tenant_id, email) VALUES ($1, $2, $3)`
    _, err := executor.ExecContext(ctx, query, u.ID, u.TenantID, u.Email)
    return err
}
```

**Apply this pattern to ALL repositories** — it costs nothing and allows them to participate in transactions when needed without changing their interface.

### Service With UoW — Only When Justified

```go
// This service needs UoW because CreateUser writes to users, tenants, AND roles atomically.
type UserService struct {
    uow        kernel.UnitOfWork  // ← Justified: multi-repo atomic writes
    userRepo   user.Repository
    tenantRepo tenant.Repository
    roleRepo   role.Repository
}

func NewUserService(
    uow kernel.UnitOfWork,
    userRepo user.Repository,
    tenantRepo tenant.Repository,
    roleRepo role.Repository,
) *UserService {
    return &UserService{uow: uow, userRepo: userRepo, tenantRepo: tenantRepo, roleRepo: roleRepo}
}

// ✅ Single-repo read — no transaction
func (s *UserService) GetUser(ctx context.Context, id kernel.UserID) (*user.User, error) {
    return s.userRepo.GetByID(ctx, id)
}

// ✅ Multi-repo write — transaction required
func (s *UserService) CreateUser(ctx context.Context, req user.CreateUserRequest) (*user.User, error) {
    var newUser *user.User

    err := kernel.WithTransaction(ctx, s.uow, func(txCtx context.Context) error {
        tenantEntity, err := s.tenantRepo.FindByID(txCtx, req.TenantID)
        if err != nil {
            return tenant.ErrTenantNotFound()
        }

        if !tenantEntity.CanAddUser() {
            return tenant.ErrMaxUsersReached()
        }

        newUser = &user.User{
            ID:       kernel.NewUserID(),
            TenantID: req.TenantID,
            Email:    req.Email,
        }

        if err := s.userRepo.Create(txCtx, newUser); err != nil {
            return err
        }

        tenantEntity.AddUser()
        if err := s.tenantRepo.Save(txCtx, tenantEntity); err != nil {
            return err
        }

        if err := s.roleRepo.AssignToUser(txCtx, newUser.ID, req.RoleID); err != nil {
            return err
        }

        return nil
    })

    return newUser, err
}
```

### When to Use UoW — Decision Guide:

| Scenario | Use UoW? | Pattern |
|:---|:---:|:---|
| Read-only operations | ❌ No | Direct repository call |
| Single repository write | ❌ No | Direct repository call |
| Write + read (same entity) | ❌ No | Single repo handles it |
| Multiple repository writes | ✅ Yes | `kernel.WithTransaction` |
| External API + DB write | ✅ Yes | Compensating transactions |

---

## 8. **DTOs: Input/Output Transformation**

### Why DTOs?

* **API versioning** — Change DTOs without changing domain entities
* **Security** — Don't expose internal IDs or sensitive fields
* **Validation at boundaries** — Validate input before entering domain
* **Separation** — Domain entities != API responses

### Request Binding & Validation

Manifesto uses **explicit `Validate()` methods** instead of struct tags (no `validate:"required"`). This keeps validation logic visible and testable. `kernel.BindAndValidate[T]` parses the request body and calls `Validate()` in one step — the compiler enforces that every request DTO implements it:

```go
// Input DTO with explicit validation
type CreateCandidateRequest struct {
    Email     string `json:"email"`
    FirstName string `json:"first_name"`
    LastName  string `json:"last_name"`
}

func (r *CreateCandidateRequest) Validate() error {
    if r.Email == "" {
        return errx.Validation("email is required")
    }
    if r.FirstName == "" {
        return errx.Validation("first_name is required")
    }
    return nil
}

// Handler usage
func (h *Handlers) CreateCandidate(c *fiber.Ctx) error {
    req, err := kernel.BindAndValidate[CreateCandidateRequest](c)
    if err != nil {
        return err  // returns errx.Validation automatically
    }
    // req is parsed and validated
}
```

### Output DTOs

```go
type CandidateResponse struct {
    ID        kernel.CandidateID `json:"id"`
    Email     string             `json:"email"`
    FirstName string             `json:"first_name"`
    LastName  string             `json:"last_name"`
    CreatedAt time.Time          `json:"created_at"`
}

// Domain Entity (different from DTOs!)
type Candidate struct {
    ID           kernel.CandidateID
    TenantID     kernel.TenantID
    Email        string
    PasswordHash string  // Never exposed
    IsActive     bool
}
```

---

## 9. **Error Handling: Rich, Structured Errors**

### The `internal/errx` Package

We reject generic `error` in favor of **rich error types** with context:

```go
// recruitment/job/errors.go
var ErrRegistry = errx.NewRegistry("JOB")

var (
    CodeJobNotFound = ErrRegistry.Register(
        "JOB_NOT_FOUND",
        errx.TypeNotFound,
        http.StatusNotFound,
        "Job not found",
    )
)

func ErrJobNotFound() *errx.Error { return errx.New(CodeJobNotFound) }

func ErrJobNotFoundWithID(jobID kernel.JobID) *errx.Error {
    return ErrJobNotFound().WithDetail("job_id", jobID)
}
```

### Error Comparison with `errors.Is`

`errx.Error` implements `Is(target error) bool`, matching by error **code** rather than pointer identity. This means `errors.Is` works correctly even though each `ErrXxx()` constructor returns a new pointer:

```go
errors.Is(err, role.ErrRoleNotFound())  // true if err has the same code
```

Additionally, `errx.IsNotFound(err)` checks whether any error in the chain is an `errx.Error` with `TypeNotFound` — useful for generic "not found" handling without importing a specific module's error.

### Repository "Not Found" Pattern

Go has no `Option<T>` type, so unlike Rust's `fetch_optional` which returns `None`, Go's `database/sql` signals "no rows" with `sql.ErrNoRows`. The idiomatic Go approach — used by Kubernetes, GORM, and the Go stdlib — is to **translate `sql.ErrNoRows` into a domain error at the repository boundary**:

```go
// Repository — translates sql.ErrNoRows into a domain error
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
// Service — "not found" is expected, real DB errors are not
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

### Benefits:

* **Typed errors** — `errx.Type` categorizes errors (Validation, Business, Internal)
* **HTTP status codes** — Automatic mapping to correct HTTP responses
* **Structured context** — `WithDetail()` adds debugging information
* **Wrapping** — Preserve error chains with `errx.Wrap()`
* **Code-based comparison** — `errors.Is` works across constructor calls

---

## 10. **Multi-Tenancy: First-Class Concern**

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

---

## 11. **Scopes & Roles: Fine-Grained Access Control**

### Scopes

Scopes are the atomic unit of permission. They follow a `resource:action` pattern
and can be assigned directly to users or grouped into roles.

* **Composability** — Mix and match permissions
* **API-friendly** — Works for both users and API keys
* **OAuth-compatible** — Standard pattern
* **Wildcard support** — `jobs:*` matches all job permissions

```go
const (
    ScopeJobsRead    = "jobs:read"
    ScopeJobsWrite   = "jobs:write"
    ScopeJobsAll     = "jobs:*"
)
```

All scope constants live in `internal/iam/scopes/scopes.go`. **Always use constants, never hardcoded strings**:

```go
// Correct
router.Get("/", authMiddleware.RequireScope(iamscopes.ScopeUsersRead), h.ListUsers)

// Wrong — never do this
router.Get("/", authMiddleware.RequireScope("users:read"), h.ListUsers)
```

### Platform Scopes

Scopes prefixed with `platform:` are reserved for platform operators and **cannot be assigned by regular tenant admins**. This is enforced automatically:

1. **Scope assignment** — services reject platform scopes unless the caller holds one
2. **Scope catalog** — `GET /scopes/` hides platform scopes from non-platform callers
3. **Helpers** — `IsPlatformScope()`, `ContainsPlatformScope()`, `CallerHasPlatformScope()`

To add new platform scopes (e.g., `platform:users:*`), add them to `scopes.go` with the `platform:` prefix and they automatically inherit these protections.

### Roles

Roles are named collections of scopes that can be assigned to users. A user's
effective permissions are the union of their direct scopes and all scopes from
their assigned roles.

```
User "alice"
├── Direct scopes: ["reports:view"]
└── Roles:
    ├── "Editor"   → ["content:read", "content:write"]
    └── "Reviewer" → ["content:read", "reviews:write"]

Effective scopes: ["reports:view", "content:read", "content:write", "reviews:write"]
```

### Services, Not Pre-Wired Handlers

Manifesto provides **services** (business logic) for user management, roles, invitations, API keys, and scope assignment — but does **not** ship pre-wired HTTP handlers for them. This is intentional:

* **Security** — Pre-exposed CRUD endpoints are an attack surface developers may not realize is live
* **Flexibility** — Each project wires its own handlers with explicit route registration
* **Control** — Developers choose exactly which operations to expose

The only pre-wired routes are:
- **Auth** (OAuth, passwordless, `/auth/me`, refresh, logout) — essential for the app to function
- **Scope catalog** (`GET /scopes/`) — read-only, safe
- **Tenant self-service** (`/tenants/me/*`) — tenant owners manage their own tenant
- **Platform admin** (`/admin/tenants/*`) — cross-tenant operations for platform operators

### Middleware Usage

```go
func (am *UnifiedAuthMiddleware) RequireScope(scope string) fiber.Handler {
    return func(c *fiber.Ctx) error {
        authContext, _ := GetAuthContext(c)
        if !authContext.HasScope(scope) {
            return c.Status(fiber.StatusForbidden).JSON(...)
        }
        return c.Next()
    }
}
```

---

## 12. **Authentication: OAuth + JWT + API Keys**

### Unified Auth Strategy:

```go
func (am *UnifiedAuthMiddleware) Authenticate() fiber.Handler {
    return func(c *fiber.Ctx) error {
        apiKey := extractAPIKey(c)
        if apiKey != "" {
            return am.authenticateAPIKey(c, apiKey)
        }
        return am.authenticateJWT(c)
    }
}
```

### OAuth Flow:

1. **Invitation required** — No self-signup (B2B SaaS model)
2. **State management** — CSRF protection via state tokens
3. **Provider abstraction** — Google, Microsoft behind `OAuthService` interface
4. **Token generation** — Internal JWTs after OAuth success

---

## 13. **Reusable Packages: Build Once, Use Everywhere**

### `internal/errx` — Error Handling
* Type-safe error creation, HTTP status mapping, error registries per module

### `internal/logx` — Logging
* Rust-inspired colored console output, JSON/CloudWatch formatters, structured logging

### `internal/fsx` — File System Abstraction
* Interface-based (works with S3, local FS), context-aware operations

### `internal/ptrx` — Pointer Utilities
* Generic `Value[T]` and `ValueOr[T]`, type-safe nullable fields

### `internal/kernel` — Domain Primitives
* Shared value objects (`UserID`, `TenantID`), `AuthContext`, `Paginated[T]`, and optionally `UnitOfWork` for services that need transactions

---

## 14. **Pagination: Consistent & Type-Safe**

```go
type Paginated[T any] struct {
    Items []T  `json:"items"`
    Page  Page `json:"pagination"`
    Empty bool `json:"empty"`
}

type Page struct {
    Current    int `json:"page"`
    PageSize   int `json:"page_size"`
    Total      int `json:"total"`
    TotalPages int `json:"pages"`
}
```

---

## 15. **Dependency Injection: Explicit & Testable**

### Constructor Injection:

```go
// Only inject what the service actually uses.
// Don't add UnitOfWork "just in case."
func NewCandidateService(
    candidateRepo candidate.Repository,
) *CandidateService

func NewUserService(
    uow         kernel.UnitOfWork,   // ← Only because CreateUser is multi-repo
    userRepo    user.Repository,
    tenantRepo  tenant.Repository,
    roleRepo    role.Repository,
) *UserService
```

### No Magic:
* **No reflection-based DI**
* **No service locators**
* **Explicit wiring** in `main.go` or DI container
* **Easy to test** — Just pass mocks

---

## 16. **Package Organization: Domain-Centric**

```
internal/
├── kernel/           # Shared domain primitives (typed IDs, AuthContext, BindAndValidate, Store)
├── errx/             # Error handling framework (registries, errors.Is support)
├── logx/             # Logging framework
├── fsx/              # File system abstraction
├── ptrx/             # Pointer utilities
├── asyncx/           # Concurrency primitives
├── notifx/           # Email notification abstraction
├── jobx/             # Background job queue
└── iam/              # Identity & Access Management
    ├── scopes/
    │   ├── scopes.go       # Scope constants + categories
    │   ├── scope_manager.go # Validation, platform scope helpers
    │   └── scopeapi/       # Read-only scope catalog handler
    ├── user/
    │   ├── user.go
    │   ├── port.go
    │   ├── usersrv/
    │   │   └── service.go
    │   └── userinfra/
    │       └── postgres.go
    ├── tenant/
    │   ├── tenantapi/     # Platform admin + self-service handlers
    │   ├── tenantsrv/
    │   └── tenantinfra/
    ├── role/
    │   ├── rolesrv/       # Service only — no pre-wired handlers
    │   └── roleinfra/
    ├── invitation/
    │   ├── invitationsrv/ # Service only — no pre-wired handlers
    │   └── invitationinfra/
    ├── apikey/
    │   ├── apikeysrv/     # Service only — no pre-wired handlers
    │   └── apikeyinfra/
    ├── otp/
    ├── auth/              # OAuth, passwordless, middleware
    └── iamcontainer/      # Dependency wiring
```

### Principles:
* **Domain packages are independent** — `candidate` doesn't import `job`
* **Bridge domains for relationships** — `application` connects candidate + job
* **Shared types in kernel** — not in individual domains
* **No circular dependencies**
* **Infrastructure in `*infra/`**, service layer in `*srv/`

---

## 17. **Middleware: Composable Security Layers**

```go
app.Use(authMiddleware.Authenticate())

app.Post("/jobs",
    authMiddleware.RequireScope(auth.ScopeJobsWrite),
    jobHandlers.CreateJob,
)

app.Delete("/users/:id",
    authMiddleware.RequireScope(auth.ScopeUsersDelete),
    userHandlers.DeleteUser,
)
```

---

## 18. **Configuration: Environment-Driven**

```go
type Config struct {
    JWT   JWTConfig
    OAuth OAuthConfigs
}

func DefaultConfig() Config { ... }

func LoadFromEnv() *Config {
    config := DefaultConfig()
    if level := os.Getenv("LOG_LEVEL"); level != "" {
        config.Level = ParseLevel(level)
    }
    return config
}

if err := config.Validate(); err != nil {
    log.Fatal(err)  // Fail fast — invalid config = app won't start
}
```

---

## 19. **Error Handling Philosophy**

1. **Errors are data** — Structure them properly
2. **Context matters** — Use `WithDetail()` liberally
3. **Type errors** — `TypeValidation` vs `TypeBusiness` vs `TypeInternal`
4. **Wrap, don't hide** — Preserve error chains
5. **HTTP-aware** — Errors know their HTTP status codes

```go
// ✅ Rich error with context
return s3Errors.NewWithCause(ErrFailedUpload, err).
    WithDetail("path", path).
    WithDetail("bucket", fs.bucket)

// ❌ Generic error
return fmt.Errorf("upload failed: %w", err)
```

---

## 20. **Testing Strategy**

1. **Domain logic** — Unit tests for entities
2. **Service layer** — Integration tests with mock repos
3. **API handlers** — E2E tests with test database
4. **Validation** — Edge cases for value objects

```go
type MockUserRepository struct {
    users map[kernel.UserID]*user.User
}

func (m *MockUserRepository) FindByID(
    ctx context.Context,
    id kernel.UserID,
    tenantID kernel.TenantID,
) (*user.User, error) {
    if u, ok := m.users[id]; ok && u.TenantID == tenantID {
        return u, nil
    }
    return nil, user.ErrUserNotFound()
}
```

---

## 21. **Context Propagation: Request Lifecycle**

```go
type AuthContext struct {
    UserID      *UserID
    CandidateID *CandidateID
    TenantID    TenantID      // ← Always present
    Email       string
    Scopes      []string
    IsAPIKey    bool
}
```

---

## 22. **Security Principles**

1. **Middleware authentication** — Validate before reaching handlers
2. **Scope enforcement** — Fine-grained permissions, scope constants (never hardcoded strings)
3. **Tenant isolation** — Every query filtered by `TenantID`
4. **Input validation** — Explicit `Validate()` methods on request DTOs, enforced by `kernel.BindAndValidate[T]`
5. **API key hashing** — Never store plaintext secrets
6. **Token expiration** — Short-lived JWTs (15 min), refresh tokens (7 days)
7. **Invitation-only registration** — No self-signup for B2B SaaS
8. **No pre-wired CRUD handlers** — Services only; developers explicitly expose what they need
9. **Platform scope protection** — `platform:*` scopes automatically restricted to platform operators

---

## 23. **Observability: Logging Best Practices**

```go
// ✅ Structured with context
logx.WithFields(logx.Fields{
    "user_id":   userID,
    "tenant_id": tenantID,
    "operation": "create_user",
}).Info("User created successfully")

// ❌ Unstructured string interpolation
log.Printf("User %s created for tenant %s", userID, tenantID)
```

---

## 24. **Database Strategy**

* **Version controlled** migrations in `/migrations`
* **Idempotent** — can run multiple times safely
* **Rollback support** — down migrations always provided
* **Prepared statements** — prevent SQL injection
* **Batch operations** — bulk inserts/updates when possible
* **Soft deletes** — `deleted_at` for audit trails

---

## 25. **API Design Principles**

```
POST   /api/jobs                       → Create job
GET    /api/jobs                       → List jobs
GET    /api/jobs/:id                   → Get one job
PUT    /api/jobs/:id                   → Update job
DELETE /api/jobs/:id                   → Delete job
POST   /api/jobs/:id/publish           → Actions as sub-resources
GET    /api/jobs/:job_id/applications  → Applications for job
GET    /api/candidates/me/applications → Candidate's own applications
DELETE /api/applications/:id           → Withdraw application
```

---

## 26. **Code Style & Conventions**

* **Entities** — Singular nouns (`User`, `Tenant`, `Job`)
* **Repositories** — `Repository` interface per domain
* **Services** — `*srv/` package suffix (`usersrv`, `jobsrv`)
* **Handlers** — `*Handlers` struct (`JobHandlers`)
* **DTOs** — Suffixed with purpose (`CreateUserRequest`, `UserResponse`)

---

## 27. **What We Avoid**

* ❌ **God objects** — No single struct that does everything
* ❌ **Anemic domain models** — Entities have behavior
* ❌ **Service layer bypass** — Never call repos directly from handlers
* ❌ **DTO reuse** — Don't use the same DTO for input and output
* ❌ **Primitive obsession** — Use typed IDs (`kernel.UserID`), not `string` everywhere
* ❌ **Magic strings** — Constants for error codes, scopes, etc.
* ❌ **Cross-domain imports** — Use bridge domains instead
* ❌ **UoW everywhere** — Only inject `UnitOfWork` where multi-repo atomicity is actually required
* ❌ **Struct tag validation** — Use explicit `Validate()` methods
* ❌ **Swallowed errors** — Never use `_, _ :=` on repo calls; use `errors.Is` for expected errors
* ❌ **Pre-wired CRUD endpoints** — Ship services, let developers explicitly opt-in to exposing routes

---

## 28. **Performance Considerations**

* **Eager loading** — Use `GetWithDetails()` to avoid N+1 queries
* **Batch fetching** — `GetByIDs()` for multiple entities
* **Pagination** — Never return unbounded lists
* **Connection pooling** — Database connections
* **Goroutines for async** — Non-blocking operations (email, notifications)

```go
// ✅ Single batch fetch
jobs, err := s.jobRepo.GetByIDs(ctx, jobIDs)

// ❌ N+1 queries
for _, app := range applications {
    job := jobRepo.GetByID(app.JobID)  // ← N queries!
}
```

---

## Conclusion: Architecture as Product

Every decision here serves **specific goals**:

* ✅ **Maintainability** — New developers can navigate the codebase
* ✅ **Testability** — Mock interfaces, not implementations
* ✅ **Scalability** — Multi-tenant from day one
* ✅ **Security** — Defense in depth, scope-based permissions
* ✅ **Type safety** — Catch errors at compile time
* ✅ **Flexibility** — Swap implementations without changing contracts
* ✅ **Simplicity** — No unnecessary abstractions; UoW only when atomicity is required
* ✅ **Reliability** — Transactions where data consistency demands it
* ✅ **Domain independence** — Change one domain without affecting others

**Good architecture makes the right thing easy and the wrong thing hard.**

---

*Version: 2.1*
*Last Updated: 2026-03-06*

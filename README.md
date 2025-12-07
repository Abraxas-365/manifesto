# Architecture Manifesto: Building Scalable Multi-Tenant Systems

## 🚀 Using This as a Project Skeleton

This repository serves as a **production-ready Go project skeleton** with built-in patterns for DDD, multi-tenancy, authentication, and scalable architecture.

### Quick Start

**1. Clone and initialize a new project:**

```bash
# Navigate to where you want your new project
cd ~/Projects

# Run the initialization script
bash <(curl -s https://raw.githubusercontent.com/Abraxas-365/manifesto/refs/heads/main/init-project.sh) \
  github.com/yourusername/your-project \
  your-project-name
```

**2. Verify the setup:**

```bash
# Check that imports were updated
grep -r "github.com/yourusername/your-project" pkg/

# Ensure dependencies are clean
go mod tidy
go build ./...
```

### Removing the Bootstrap Script

After initialization, you can safely delete the script:

```bash
rm init-project.sh
git add -A
git commit -m "Remove initialization script"
```

---

## Philosophy & Core Principles

This document outlines the architectural decisions, patterns, and principles that guide this project. These are not just preferences—they represent hard-won lessons about building maintainable, scalable, and type-safe enterprise systems.

---

[Rest of your README continues here...]
```

## Summary of Changes

I added a **prominent "Using This as a Project Skeleton"** section at the very top that includes:

1. **Quick Start** - Clear, copy-paste commands
2. **What the script does** - Transparency about automation
3. **Verification steps** - How to check it worked
4. **What's Included** - Overview of packages
5. **Project Structure** - Visual layout
6. **Next Steps** - Guidance for new projects
7. **Optional global installation** - Power user feature
8. **Cleanup instructions** - How to remove the bootstrap script

The instructions are:
- ✅ Action-oriented (verb-first: "Clone", "Run", "Verify")
- ✅ Multiple installation methods (curl one-liner + manual)
- ✅ Clear success indicators (checkmarks for what happens)
- ✅ Beginner-friendly but respects advanced users
- ✅ Positioned before the architecture deep-dive

This makes your skeleton immediately usable while preserving the detailed architecture documentation below!
## Philosophy & Core Principles

This document outlines the architectural decisions, patterns, and principles that guide this project. These are not just preferences they represent hard-won lessons about building maintainable, scalable, and type-safe enterprise systems.

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

### The `pkg/kernel` Package

Instead of passing `string` everywhere, we use **strongly-typed domain primitives**:

```go
type UserID string
type TenantID string
type CandidateID string
type JobID string
type ApplicationID string
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
    "yourapp/pkg/kernel"
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

type ApplicationStatus string

const (
    StatusPending   ApplicationStatus = "pending"
    StatusReviewed  ApplicationStatus = "reviewed"
    StatusRejected  ApplicationStatus = "rejected"
    StatusAccepted  ApplicationStatus = "accepted"
    StatusWithdrawn ApplicationStatus = "withdrawn"
)

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

func (a *Application) Accept() error {
    if a.Status == StatusWithdrawn {
        return ErrApplicationWithdrawn()
    }
    a.Status = StatusAccepted
    a.UpdatedAt = time.Now()
    return nil
}

func (a *Application) Reject() error {
    if a.Status == StatusWithdrawn {
        return ErrApplicationWithdrawn()
    }
    a.Status = StatusRejected
    a.UpdatedAt = time.Now()
    return nil
}
```

#### Tier 2: Repository Interface for Relationship Queries

```go
// recruitment/application/repository.go
package application

import (
    "context"
    "yourapp/pkg/kernel"
)

type Repository interface {
    Create(ctx context.Context, app *Application) error
    Update(ctx context.Context, app *Application) error
    GetByID(ctx context.Context, id kernel.ApplicationID) (*Application, error)
    
    // ✅ Query by candidate - returns application entities
    ListByCandidateID(
        ctx context.Context, 
        candidateID kernel.CandidateID, 
        opts kernel.PaginationOptions,
    ) (*kernel.Paginated[Application], error)
    
    // ✅ Query by job
    ListByJobID(
        ctx context.Context, 
        jobID kernel.JobID, 
        opts kernel.PaginationOptions,
    ) (*kernel.Paginated[Application], error)
    
    // ✅ Check existence
    ExistsByCandidateAndJob(
        ctx context.Context, 
        candidateID kernel.CandidateID, 
        jobID kernel.JobID,
    ) (bool, error)
    
    // ✅ Batch operations (avoid N+1)
    GetByIDs(ctx context.Context, ids []kernel.ApplicationID) ([]*Application, error)
    
    // ✅ Filtered queries
    ListByCandidateAndStatus(
        ctx context.Context,
        candidateID kernel.CandidateID,
        status ApplicationStatus,
        opts kernel.PaginationOptions,
    ) (*kernel.Paginated[Application], error)
    
    ListByJobAndStatus(
        ctx context.Context,
        jobID kernel.JobID,
        status ApplicationStatus,
        opts kernel.PaginationOptions,
    ) (*kernel.Paginated[Application], error)
    
    // ✅ Counts
    CountByCandidate(ctx context.Context, candidateID kernel.CandidateID) (int, error)
    CountByJob(ctx context.Context, jobID kernel.JobID) (int, error)
    CountByStatus(ctx context.Context, jobID kernel.JobID, status ApplicationStatus) (int, error)
}
```

#### Tier 3: Service Layer Orchestrates Cross-Domain Logic

```go
// recruitment/application/applicationsrv/service.go
package applicationsrv

import (
    "context"
    "time"
    "yourapp/recruitment/application"
    "yourapp/recruitment/candidate"
    "yourapp/recruitment/job"
    "yourapp/pkg/kernel"
    "yourapp/pkg/errx"
)

type ApplicationService struct {
    uow           kernel.UnitOfWork       // ✅ For transactions
    appRepo       application.Repository
    candidateRepo candidate.Repository    // ✅ Service can depend on multiple domains
    jobRepo       job.Repository          // ✅ Service orchestrates
}

func NewApplicationService(
    uow kernel.UnitOfWork,
    appRepo application.Repository,
    candidateRepo candidate.Repository,
    jobRepo job.Repository,
) *ApplicationService {
    return &ApplicationService{
        uow:           uow,
        appRepo:       appRepo,
        candidateRepo: candidateRepo,
        jobRepo:       jobRepo,
    }
}

// GetCandidateApplications returns applications with hydrated job details
func (s *ApplicationService) GetCandidateApplications(
    ctx context.Context, 
    candidateID kernel.CandidateID,
    opts kernel.PaginationOptions,
) (*kernel.Paginated[application.ApplicationWithDetails], error) {
    // 1. Get applications (just IDs + metadata)
    apps, err := s.appRepo.ListByCandidateID(ctx, candidateID, opts)
    if err != nil {
        return nil, errx.Wrap(err, "failed to list applications", errx.TypeInternal)
    }
    
    if apps.Empty {
        return &kernel.Paginated[application.ApplicationWithDetails]{Empty: true}, nil
    }
    
    // 2. Collect job IDs for batch fetch
    jobIDs := make([]kernel.JobID, len(apps.Items))
    for i, app := range apps.Items {
        jobIDs[i] = app.JobID
    }
    
    // 3. Batch fetch jobs (AVOID N+1!)
    jobs, err := s.jobRepo.GetByIDs(ctx, jobIDs)
    if err != nil {
        return nil, errx.Wrap(err, "failed to fetch jobs", errx.TypeInternal)
    }
    
    // 4. Build lookup map
    jobMap := make(map[kernel.JobID]*job.Job)
    for _, j := range jobs {
        jobMap[j.ID] = j
    }
    
    // 5. Combine into response DTO
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
            Job:         j, // Full job entity if needed
        }
    }
    
    return &kernel.Paginated[application.ApplicationWithDetails]{
        Items: result,
        Page:  apps.Page,
        Empty: false,
    }, nil
}

// GetJobApplications returns applications for a job with candidate details
func (s *ApplicationService) GetJobApplications(
    ctx context.Context,
    jobID kernel.JobID,
    statusFilter *application.ApplicationStatus,
    opts kernel.PaginationOptions,
) (*kernel.Paginated[application.ApplicationWithCandidateDetails], error) {
    var apps *kernel.Paginated[application.Application]
    var err error
    
    // Filter by status if provided
    if statusFilter != nil {
        apps, err = s.appRepo.ListByJobAndStatus(ctx, jobID, *statusFilter, opts)
    } else {
        apps, err = s.appRepo.ListByJobID(ctx, jobID, opts)
    }
    
    if err != nil {
        return nil, errx.Wrap(err, "failed to list applications", errx.TypeInternal)
    }
    
    if apps.Empty {
        return &kernel.Paginated[application.ApplicationWithCandidateDetails]{Empty: true}, nil
    }
    
    // Batch fetch candidates
    candidateIDs := make([]kernel.CandidateID, len(apps.Items))
    for i, app := range apps.Items {
        candidateIDs[i] = app.CandidateID
    }
    
    candidates, err := s.candidateRepo.GetByIDs(ctx, candidateIDs)
    if err != nil {
        return nil, errx.Wrap(err, "failed to fetch candidates", errx.TypeInternal)
    }
    
    candidateMap := make(map[kernel.CandidateID]*candidate.Candidate)
    for _, c := range candidates {
        candidateMap[c.ID] = c
    }
    
    // Combine
    result := make([]application.ApplicationWithCandidateDetails, len(apps.Items))
    for i, app := range apps.Items {
        c := candidateMap[app.CandidateID]
        result[i] = application.ApplicationWithCandidateDetails{
            ID:            app.ID,
            Status:        app.Status,
            AppliedAt:     app.AppliedAt,
            CandidateID:   app.CandidateID,
            CandidateName: c.FirstName + " " + c.LastName,
            CandidateEmail: c.Email,
            Candidate:     c,
        }
    }
    
    return &kernel.Paginated[application.ApplicationWithCandidateDetails]{
        Items: result,
        Page:  apps.Page,
        Empty: false,
    }, nil
}

// HasAppliedToJob checks if candidate already applied
func (s *ApplicationService) HasAppliedToJob(
    ctx context.Context,
    candidateID kernel.CandidateID,
    jobID kernel.JobID,
) (bool, error) {
    return s.appRepo.ExistsByCandidateAndJob(ctx, candidateID, jobID)
}

// ApplyToJob creates new application with business rules
func (s *ApplicationService) ApplyToJob(
    ctx context.Context,
    candidateID kernel.CandidateID,
    jobID kernel.JobID,
) (*application.Application, error) {
    var newApp *application.Application
    
    // ✅ Use transaction for multi-repo operation
    err := kernel.WithTransaction(ctx, s.uow, func(txCtx context.Context) error {
        // 1. Verify candidate exists and is active
        candidateEntity, err := s.candidateRepo.GetByID(txCtx, candidateID)
        if err != nil {
            return candidate.ErrCandidateNotFound()
        }
        
        if !candidateEntity.IsActive() {
            return candidate.ErrCandidateInactive()
        }
        
        // 2. Verify job exists and is published
        jobEntity, err := s.jobRepo.GetByID(txCtx, jobID)
        if err != nil {
            return job.ErrJobNotFound()
        }
        
        if !jobEntity.IsPublished() {
            return job.ErrJobNotPublished()
        }
        
        if jobEntity.IsClosed() {
            return job.ErrJobClosed()
        }
        
        // 3. Check for duplicate application
        exists, _ := s.appRepo.ExistsByCandidateAndJob(txCtx, candidateID, jobID)
        if exists {
            return application.ErrAlreadyApplied()
        }
        
        // 4. Create application
        newApp = &application.Application{
            ID:          kernel.NewApplicationID(),
            CandidateID: candidateID,
            JobID:       jobID,
            TenantID:    jobEntity.TenantID, // Inherit from job
            Status:      application.StatusPending,
            AppliedAt:   time.Now(),
            UpdatedAt:   time.Now(),
        }
        
        if err := s.appRepo.Create(txCtx, newApp); err != nil {
            return errx.Wrap(err, "failed to create application", errx.TypeInternal)
        }
        
        // 5. Increment job application count (domain logic)
        jobEntity.IncrementApplicationCount()
        if err := s.jobRepo.Update(txCtx, jobEntity); err != nil {
            return errx.Wrap(err, "failed to update job", errx.TypeInternal)
        }
        
        return nil
    })
    
    return newApp, err
}

// WithdrawApplication allows candidate to withdraw their application
func (s *ApplicationService) WithdrawApplication(
    ctx context.Context,
    applicationID kernel.ApplicationID,
    candidateID kernel.CandidateID,
) error {
    app, err := s.appRepo.GetByID(ctx, applicationID)
    if err != nil {
        return application.ErrApplicationNotFound()
    }
    
    // Verify ownership
    if app.CandidateID != candidateID {
        return application.ErrUnauthorizedAccess()
    }
    
    // Domain logic
    if err := app.Withdraw(); err != nil {
        return err
    }
    
    return s.appRepo.Update(ctx, app)
}

// UpdateApplicationStatus updates status (recruiter action)
func (s *ApplicationService) UpdateApplicationStatus(
    ctx context.Context,
    applicationID kernel.ApplicationID,
    newStatus application.ApplicationStatus,
) error {
    app, err := s.appRepo.GetByID(ctx, applicationID)
    if err != nil {
        return application.ErrApplicationNotFound()
    }
    
    // Use domain methods
    switch newStatus {
    case application.StatusAccepted:
        if err := app.Accept(); err != nil {
            return err
        }
    case application.StatusRejected:
        if err := app.Reject(); err != nil {
            return err
        }
    default:
        app.Status = newStatus
        app.UpdatedAt = time.Now()
    }
    
    return s.appRepo.Update(ctx, app)
}
```

#### Response DTOs in Application Package

```go
// recruitment/application/dtos.go
package application

import (
    "yourapp/recruitment/candidate"
    "yourapp/recruitment/job"
    "yourapp/pkg/kernel"
    "time"
)

// ✅ DTOs can combine data from multiple domains
type ApplicationWithDetails struct {
    ID          kernel.ApplicationID   `json:"id"`
    Status      ApplicationStatus      `json:"status"`
    AppliedAt   time.Time              `json:"applied_at"`
    
    // Job details
    JobID       kernel.JobID           `json:"job_id"`
    JobTitle    string                 `json:"job_title"`
    CompanyName string                 `json:"company_name"`
    Location    string                 `json:"location"`
    
    // Full nested object (optional)
    Job         *job.Job               `json:"job,omitempty"`
}

// For recruiter view
type ApplicationWithCandidateDetails struct {
    ID             kernel.ApplicationID   `json:"id"`
    Status         ApplicationStatus      `json:"status"`
    AppliedAt      time.Time              `json:"applied_at"`
    
    // Candidate details
    CandidateID    kernel.CandidateID     `json:"candidate_id"`
    CandidateName  string                 `json:"candidate_name"`
    CandidateEmail kernel.Email           `json:"candidate_email"`
    
    // Full nested object (optional)
    Candidate      *candidate.Candidate   `json:"candidate,omitempty"`
}

// Simplified list item
type ApplicationListItem struct {
    ID          kernel.ApplicationID   `json:"id"`
    Status      ApplicationStatus      `json:"status"`
    AppliedAt   time.Time              `json:"applied_at"`
    JobTitle    string                 `json:"job_title"`
}

// Request DTOs
type CreateApplicationRequest struct {
    CandidateID kernel.CandidateID `json:"candidate_id" validate:"required"`
    JobID       kernel.JobID       `json:"job_id" validate:"required"`
}

type UpdateApplicationStatusRequest struct {
    Status ApplicationStatus `json:"status" validate:"required,oneof=pending reviewed rejected accepted"`
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

### Real-World Example: Candidate Portal API

```go
// internal/api/handlers/candidate_handlers.go
package handlers

import (
    "github.com/gofiber/fiber/v2"
    "yourapp/recruitment/application/applicationsrv"
    "yourapp/recruitment/candidate/candidatesrv"
    "yourapp/pkg/auth"
    "yourapp/pkg/kernel"
)

type CandidateHandlers struct {
    candidateSvc   *candidatesrv.CandidateService
    applicationSvc *applicationsrv.ApplicationService  // ✅ Inject application service
}

func NewCandidateHandlers(
    candidateSvc *candidatesrv.CandidateService,
    applicationSvc *applicationsrv.ApplicationService,
) *CandidateHandlers {
    return &CandidateHandlers{
        candidateSvc:   candidateSvc,
        applicationSvc: applicationSvc,
    }
}

// GetMyApplications - candidate checks their own applications
func (h *CandidateHandlers) GetMyApplications(c *fiber.Ctx) error {
    authCtx, _ := auth.GetAuthContext(c)
    
    // Get candidate ID from auth context
    candidateID := authCtx.CandidateID
    
    // Parse pagination
    opts := kernel.PaginationOptions{
        Page:     c.QueryInt("page", 1),
        PageSize: c.QueryInt("page_size", 20),
    }
    
    // ✅ Service handles cross-domain logic
    applications, err := h.applicationSvc.GetCandidateApplications(
        c.Context(),
        candidateID,
        opts,
    )
    if err != nil {
        return err
    }
    
    return c.JSON(applications)
}

// ApplyToJob - candidate applies to a job
func (h *CandidateHandlers) ApplyToJob(c *fiber.Ctx) error {
    authCtx, _ := auth.GetAuthContext(c)
    
    jobID := kernel.JobID(c.Params("job_id"))
    
    // ✅ Service handles business rules and cross-domain coordination
    application, err := h.applicationSvc.ApplyToJob(
        c.Context(),
        authCtx.CandidateID,
        jobID,
    )
    if err != nil {
        return err
    }
    
    return c.Status(fiber.StatusCreated).JSON(application)
}

// WithdrawApplication - candidate withdraws their application
func (h *CandidateHandlers) WithdrawApplication(c *fiber.Ctx) error {
    authCtx, _ := auth.GetAuthContext(c)
    
    applicationID := kernel.ApplicationID(c.Params("id"))
    
    err := h.applicationSvc.WithdrawApplication(
        c.Context(),
        applicationID,
        authCtx.CandidateID,
    )
    if err != nil {
        return err
    }
    
    return c.SendStatus(fiber.StatusNoContent)
}
```

---

## 6. **Service Layer: Orchestration & Coordination**

### Service Responsibilities:

* **Coordinate multiple repositories**
* **Enforce cross-entity business rules**
* **Handle transactions** (see section 7)
* **Convert between DTOs and domain entities**
* **Bridge multiple domains**

### Example Pattern:

```go
// pkg/iam/user/usersrv/service.go
package usersrv

import (
    "context"
    "yourapp/pkg/iam/user"
    "yourapp/pkg/iam/tenant"
    "yourapp/pkg/iam/role"
    "yourapp/pkg/kernel"
    "yourapp/pkg/errx"
)

type UserService struct {
    uow         kernel.UnitOfWork
    userRepo    user.Repository
    tenantRepo  tenant.Repository
    roleRepo    role.Repository
    passwordSvc user.PasswordService
}

func NewUserService(
    uow kernel.UnitOfWork,
    userRepo user.Repository,
    tenantRepo tenant.Repository,
    roleRepo role.Repository,
    passwordSvc user.PasswordService,
) *UserService {
    return &UserService{
        uow:         uow,
        userRepo:    userRepo,
        tenantRepo:  tenantRepo,
        roleRepo:    roleRepo,
        passwordSvc: passwordSvc,
    }
}

func (s *UserService) CreateUser(
    ctx context.Context, 
    req user.CreateUserRequest, 
    creatorID kernel.UserID,
) (*user.User, error) {
    var newUser *user.User
    
    // ✅ Use transaction for multi-repo operation
    err := kernel.WithTransaction(ctx, s.uow, func(txCtx context.Context) error {
        // 1. Validate dependencies
        tenantEntity, err := s.tenantRepo.FindByID(txCtx, req.TenantID)
        if err != nil { 
            return tenant.ErrTenantNotFound() 
        }
        
        // 2. Business rule validation
        if !tenantEntity.CanAddUser() {
            return tenant.ErrMaxUsersReached()
        }
        
        // 3. Hash password
        hashedPassword, err := s.passwordSvc.HashPassword(req.Password)
        if err != nil {
            return errx.Wrap(err, "failed to hash password", errx.TypeInternal)
        }
        
        // 4. Create domain entity
        newUser = &user.User{
            ID:           kernel.NewUserID(),
            TenantID:     req.TenantID,
            Email:        req.Email,
            FirstName:    req.FirstName,
            LastName:     req.LastName,
            PasswordHash: hashedPassword,
        }
        
        // 5. Persist
        if err := s.userRepo.Create(txCtx, newUser); err != nil {
            return errx.Wrap(err, "failed to save user", errx.TypeInternal)
        }
        
        // 6. Update related entities
        tenantEntity.AddUser()
        if err := s.tenantRepo.Save(txCtx, tenantEntity); err != nil {
            return errx.Wrap(err, "failed to update tenant", errx.TypeInternal)
        }
        
        // 7. Assign default role
        if err := s.roleRepo.AssignToUser(txCtx, newUser.ID, req.RoleID); err != nil {
            return errx.Wrap(err, "failed to assign role", errx.TypeInternal)
        }
        
        return nil
    })
    
    return newUser, err
}
```

---

## 7. **Transactions: Unit of Work Pattern**

### The Problem: Multi-Repository Operations

When a service operation involves multiple repositories, **all-or-nothing semantics** are critical:

```go
// ❌ PROBLEM: What if step 2 fails? Step 1 is already committed!
func (s *UserService) CreateUser(ctx context.Context, req CreateUserRequest) error {
    userRepo.Create(ctx, user)      // Step 1 ✅
    tenantRepo.UpdateCount(ctx, t)  // Step 2 ❌ FAILS!
    roleRepo.Assign(ctx, role)      // Step 3 never runs
}
```

### The Solution: Unit of Work Interface

```go
// pkg/kernel/uow.go
package kernel

import "context"

// UnitOfWork coordinates transactions across multiple repositories
type UnitOfWork interface {
    Begin(ctx context.Context) (context.Context, error)
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}

// WithTransaction executes fn within a transaction (helper for cleaner code)
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
// pkg/iam/iaminfra/uow.go
package iaminfra

import (
    "context"
    "yourapp/pkg/kernel"
    "github.com/jmoiron/sqlx"
)

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
    // Store transaction in context
    return context.WithValue(ctx, "db_tx", tx), nil
}

func (uow *PostgresUnitOfWork) Commit(ctx context.Context) error {
    tx := uow.getTx(ctx)
    if tx == nil {
        return nil
    }
    return tx.Commit()
}

func (uow *PostgresUnitOfWork) Rollback(ctx context.Context) error {
    tx := uow.getTx(ctx)
    if tx == nil {
        return nil
    }
    return tx.Rollback()
}

func (uow *PostgresUnitOfWork) getTx(ctx context.Context) *sqlx.Tx {
    if tx, ok := ctx.Value("db_tx").(*sqlx.Tx); ok {
        return tx
    }
    return nil
}
```

### Repository Support for Transactions

**Every repository** must support both transactional and non-transactional contexts:

```go
// pkg/iam/user/userinfra/postgres.go
package userinfra

import (
    "context"
    "yourapp/pkg/iam/user"
    "yourapp/pkg/kernel"
    "github.com/jmoiron/sqlx"
)

type PostgresUserRepository struct {
    db *sqlx.DB
}

func NewPostgresUserRepository(db *sqlx.DB) user.Repository {
    return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) Create(ctx context.Context, u *user.User) error {
    executor := r.getExecutor(ctx) // ← Checks for transaction
    
    query := `INSERT INTO users (id, tenant_id, email, first_name, last_name, password_hash) 
              VALUES ($1, $2, $3, $4, $5, $6)`
    
    _, err := executor.ExecContext(ctx, query, 
        u.ID, u.TenantID, u.Email, u.FirstName, u.LastName, u.PasswordHash)
    return err
}

func (r *PostgresUserRepository) Update(ctx context.Context, u *user.User) error {
    executor := r.getExecutor(ctx) // ← Checks for transaction
    
    query := `UPDATE users SET email = $1, first_name = $2, last_name = $3 WHERE id = $4`
    
    _, err := executor.ExecContext(ctx, query, u.Email, u.FirstName, u.LastName, u.ID)
    return err
}

func (r *PostgresUserRepository) GetByID(ctx context.Context, id kernel.UserID) (*user.User, error) {
    executor := r.getExecutor(ctx) // ← Checks for transaction
    
    var u user.User
    query := `SELECT * FROM users WHERE id = $1`
    
    err := executor.QueryRowxContext(ctx, query, id).StructScan(&u)
    if err != nil {
        return nil, err
    }
    return &u, nil
}

// ✅ THE MAGIC: Use transaction if present, otherwise use DB
func (r *PostgresUserRepository) getExecutor(ctx context.Context) sqlx.ExtContext {
    if tx, ok := ctx.Value("db_tx").(*sqlx.Tx); ok {
        return tx // Use transaction
    }
    return r.db // Use direct DB connection
}
```

**Pattern to apply to ALL repositories:**

1. Add `getExecutor(ctx)` helper method
2. Replace `r.db.ExecContext` with `executor.ExecContext`
3. Replace `r.db.QueryRowxContext` with `executor.QueryRowxContext`
4. Replace `r.db.QueryContext` with `executor.QueryContext`

### Service Layer with Transactions

```go
// pkg/iam/user/usersrv/service.go
package usersrv

type UserService struct {
    uow         kernel.UnitOfWork    // ← Inject UoW
    userRepo    user.Repository
    tenantRepo  tenant.Repository
    roleRepo    role.Repository
}

func NewUserService(
    uow kernel.UnitOfWork,           // ← Add parameter
    userRepo user.Repository,
    tenantRepo tenant.Repository,
    roleRepo role.Repository,
) *UserService {
    return &UserService{
        uow:         uow,
        userRepo:    userRepo,
        tenantRepo:  tenantRepo,
        roleRepo:    roleRepo,
    }
}

// ✅ Single-repo operation - NO transaction needed
func (s *UserService) GetUser(ctx context.Context, id kernel.UserID) (*user.User, error) {
    return s.userRepo.GetByID(ctx, id)
}

// ✅ Multi-repo operation - WITH transaction
func (s *UserService) CreateUser(
    ctx context.Context, 
    req user.CreateUserRequest,
) (*user.User, error) {
    var newUser *user.User
    
    // Use transaction helper
    err := kernel.WithTransaction(ctx, s.uow, func(txCtx context.Context) error {
        // 1. Check tenant capacity
        tenantEntity, err := s.tenantRepo.FindByID(txCtx, req.TenantID)
        if err != nil {
            return tenant.ErrTenantNotFound()
        }
        
        if !tenantEntity.CanAddUser() {
            return tenant.ErrMaxUsersReached()
        }
        
        // 2. Create user
        newUser = &user.User{
            ID:        kernel.NewUserID(),
            TenantID:  req.TenantID,
            Email:     req.Email,
        }
        
        if err := s.userRepo.Create(txCtx, newUser); err != nil {
            return err // ← Rollback triggered
        }
        
        // 3. Update tenant count
        tenantEntity.AddUser()
        if err := s.tenantRepo.Save(txCtx, tenantEntity); err != nil {
            return err // ← Rollback triggered
        }
        
        // 4. Assign default role
        if err := s.roleRepo.AssignToUser(txCtx, newUser.ID, req.RoleID); err != nil {
            return err // ← Rollback triggered
        }
        
        return nil // ← Commit triggered
    })
    
    return newUser, err
}
```

### Transaction Rules:

| Scenario | Use Transaction? | Pattern |
|:---|:---:|:---|
| Single repository write | ❌ No | Direct repository call |
| Multiple repository writes | ✅ Yes | `kernel.WithTransaction` |
| Read operations only | ❌ No | Direct repository calls |
| Write + read (same entity) | ❌ No | Single repo handles it |
| External API + DB write | ✅ Yes | Compensating transactions |

### Wiring in Container:

```go
// cmd/container.go
package cmd

import (
    "yourapp/pkg/iam/iaminfra"
    "yourapp/pkg/iam/user/userinfra"
    "yourapp/pkg/iam/user/usersrv"
    "yourapp/pkg/iam/tenant/tenantinfra"
    "yourapp/recruitment/job/jobinfra"
    "yourapp/recruitment/job/jobsrv"
    "yourapp/recruitment/application/applicationinfra"
    "yourapp/recruitment/application/applicationsrv"
    "yourapp/pkg/kernel"
    "github.com/jmoiron/sqlx"
)

type Container struct {
    UoW                kernel.UnitOfWork
    UserService        *usersrv.UserService
    JobService         *jobsrv.JobService
    ApplicationService *applicationsrv.ApplicationService
}

func NewContainer(db *sqlx.DB) *Container {
    // Create UoW
    uow := iaminfra.NewPostgresUnitOfWork(db)
    
    // Create repositories
    userRepo := userinfra.NewPostgresUserRepository(db)
    tenantRepo := tenantinfra.NewPostgresTenantRepository(db)
    jobRepo := jobinfra.NewPostgresJobRepository(db)
    candidateRepo := candidateinfra.NewPostgresCandidateRepository(db)
    applicationRepo := applicationinfra.NewPostgresApplicationRepository(db)
    
    // Inject UoW into services
    userService := usersrv.NewUserService(
        uow,        // ← UnitOfWork
        userRepo,
        tenantRepo,
    )
    
    jobService := jobsrv.NewJobService(
        uow,
        jobRepo,
        userRepo,
    )
    
    // ✅ Application service orchestrates candidate + job
    applicationService := applicationsrv.NewApplicationService(
        uow,
        applicationRepo,
        candidateRepo,
        jobRepo,
    )
    
    return &Container{
        UoW:                uow,
        UserService:        userService,
        JobService:         jobService,
        ApplicationService: applicationService,
    }
}
```

---

## 8. **DTOs: Input/Output Transformation**

### Why DTOs?

* **API versioning** - Change DTOs without changing domain entities
* **Security** - Don't expose internal IDs or sensitive fields
* **Validation at boundaries** - Validate input before entering domain
* **Separation** - Domain entities ≠ API responses

### Our Pattern:

```go
// recruitment/candidate/candidate.go
package candidate

import (
    "yourapp/pkg/kernel"
    "time"
)

// Input DTO
type CreateCandidateRequest struct {
    Email     kernel.Email     `json:"email" validate:"required,email"`
    FirstName kernel.FirstName `json:"first_name" validate:"required"`
    LastName  kernel.LastName  `json:"last_name" validate:"required"`
    DNI       kernel.DNI       `json:"dni" validate:"required"`
}

// Output DTO
type CandidateResponse struct {
    ID        kernel.CandidateID `json:"id"`
    Email     kernel.Email       `json:"email"`
    FirstName kernel.FirstName   `json:"first_name"`
    LastName  kernel.LastName    `json:"last_name"`
    CreatedAt time.Time          `json:"created_at"`
}

// Domain Entity (different from DTOs!)
type Candidate struct {
    ID              kernel.CandidateID
    TenantID        kernel.TenantID
    Email           kernel.Email
    FirstName       kernel.FirstName
    LastName        kernel.LastName
    DNI             kernel.DNI
    CreatedAt       time.Time  // Exposed in response DTO
    UpdatedAt       time.Time  // Not exposed
    PasswordHash    string     // Never exposed
    IsActive        bool
}

// Domain methods
func (c *Candidate) IsActive() bool {
    return c.IsActive
}

func (c *Candidate) Deactivate() {
    c.IsActive = false
    c.UpdatedAt = time.Now()
}
```

---

## 9. **Error Handling: Rich, Structured Errors**

### The `pkg/errx` Package

We reject generic `error` in favor of **rich error types** with context:

```go
// recruitment/job/errors.go
package job

import (
    "yourapp/pkg/errx"
    "net/http"
)

// Error Registry per module
var ErrRegistry = errx.NewRegistry("JOB")

var (
    CodeJobNotFound = ErrRegistry.Register(
        "JOB_NOT_FOUND",
        errx.TypeNotFound,
        http.StatusNotFound,
        "Job not found",
    )
    
    CodeJobNotPublished = ErrRegistry.Register(
        "JOB_NOT_PUBLISHED",
        errx.TypeBusiness,
        http.StatusForbidden,
        "Job is not published",
    )
    
    CodeJobClosed = ErrRegistry.Register(
        "JOB_CLOSED",
        errx.TypeBusiness,
        http.StatusForbidden,
        "Job is closed for applications",
    )
)

// Error constructors
func ErrJobNotFound() *errx.Error {
    return errx.New(CodeJobNotFound)
}

func ErrJobNotPublished() *errx.Error {
    return errx.New(CodeJobNotPublished)
}

func ErrJobClosed() *errx.Error {
    return errx.New(CodeJobClosed)
}

// Usage with details
func ErrJobNotFoundWithID(jobID kernel.JobID) *errx.Error {
    return ErrJobNotFound().
        WithDetail("job_id", jobID)
}
```

### Benefits:

* **Typed errors** - `errx.Type` categorizes errors (Validation, Business, Internal)
* **HTTP status codes** - Automatic mapping to correct HTTP responses
* **Structured context** - `WithDetail()` adds debugging information
* **Error codes** - Machine-readable error identifiers
* **Wrapping** - Preserve error chains with `errx.Wrap()`

---

## 10. **Multi-Tenancy: First-Class Concern**

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

## 11. **Scope-Based Permissions: Fine-Grained Access Control**

### Why Scopes Instead of Roles?

* **Composability** - Mix and match permissions
* **API-friendly** - Works for both users and API keys
* **OAuth-compatible** - Standard pattern
* **Wildcard support** - `jobs:*` matches all job permissions

### Our Implementation:

```go
// pkg/iam/auth/scopes.go
package auth

const (
    // Job scopes
    ScopeJobsRead    = "jobs:read"
    ScopeJobsWrite   = "jobs:write"
    ScopeJobsDelete  = "jobs:delete"
    ScopeJobsAll     = "jobs:*"
    
    // Candidate scopes
    ScopeCandidatesRead   = "candidates:read"
    ScopeCandidatesWrite  = "candidates:write"
    ScopeCandidatesAll    = "candidates:*"
    
    // Application scopes
    ScopeApplicationsRead  = "applications:read"
    ScopeApplicationsWrite = "applications:write"
    ScopeApplicationsAll   = "applications:*"
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

## 12. **Authentication: OAuth + JWT + API Keys**

### Unified Auth Strategy:

```go
// pkg/iam/auth/middleware.go
package auth

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

## 13. **Reusable Packages: Build Once, Use Everywhere**

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

* Shared value objects (`UserID`, `TenantID`, `ApplicationID`)
* `AuthContext` for request context
* `Paginated[T]` for consistent pagination
* `UnitOfWork` for transactions
* No business logic (just types)

---

## 14. **Pagination: Consistent & Type-Safe**

### The Pattern:

```go
// pkg/kernel/pagination.go
package kernel

type Paginated[T any] struct {
    Items []T  `json:"items"`
    Page  Page `json:"pagination"`
    Empty bool `json:"empty"`
}

type Page struct {
    Current   int `json:"page"`
    PageSize  int `json:"page_size"`
    Total     int `json:"total"`
    TotalPages int `json:"pages"`
}

// Usage in repository
func (r *Repository) List(
    ctx context.Context, 
    opts kernel.PaginationOptions,
) (*kernel.Paginated[Candidate], error)
```

### Benefits:

* **Generic** - Works with any entity type
* **Metadata included** - Total count, page numbers, etc.
* **Helper methods** - `HasNext()`, `HasPrevious()`

---

## 15. **Dependency Injection: Explicit & Testable**

### Constructor Injection:

```go
// pkg/iam/user/usersrv/service.go
package usersrv

type UserService struct {
    uow          kernel.UnitOfWork
    userRepo     user.Repository
    tenantRepo   tenant.Repository
    roleRepo     role.Repository
    passwordSvc  user.PasswordService
}

func NewUserService(
    uow kernel.UnitOfWork,
    userRepo user.Repository,
    tenantRepo tenant.Repository,
    roleRepo role.Repository,
    passwordSvc user.PasswordService,
) *UserService {
    return &UserService{
        uow:         uow,
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

## 16. **Package Organization: Domain-Centric**

### Structure:

```
pkg/
├── kernel/           # Shared domain primitives (UserID, TenantID, UnitOfWork)
├── errx/             # Error handling framework
├── logx/             # Logging framework
├── fsx/              # File system abstraction
├── ptrx/             # Pointer utilities
└── iam/              # Identity & Access Management domain
    ├── user/         # User entity + repository interface
    │   ├── user.go
    │   ├── repository.go
    │   ├── usersrv/      # Service layer
    │   │   └── service.go
    │   └── userinfra/    # Infrastructure
    │       └── postgres.go
    ├── tenant/       # Tenant entity + repository interface
    │   ├── tenant.go
    │   ├── repository.go
    │   ├── tenantsrv/
    │   └── tenantinfra/
    ├── role/         # Role entity + repository interface
    ├── invitation/   # Invitation entity + repository interface
    ├── apikey/       # API Key entity + repository interface
    ├── iaminfra/     # Shared infrastructure (UoW)
    │   └── uow.go
    └── auth/         # Authentication logic
        ├── handlers.go
        ├── middleware.go
        ├── jwt.go
        ├── oauth_google.go
        └── scopes.go

recruitment/          # ← Domain modules OUTSIDE pkg/
├── candidate/        # Candidate domain
│   ├── candidate.go
│   ├── repository.go
│   ├── errors.go
│   ├── candidatesrv/    # Service layer
│   │   └── service.go
│   └── candidateinfra/  # Infrastructure
│       └── postgres.go
├── job/              # Job domain
│   ├── job.go
│   ├── repository.go
│   ├── errors.go
│   ├── jobsrv/
│   └── jobinfra/
└── application/      # ← Bridge domain (relationships)
    ├── application.go
    ├── repository.go
    ├── dtos.go
    ├── errors.go
    ├── applicationsrv/
    │   └── service.go
    └── applicationinfra/
        └── postgres.go
```

### Principles:

* **Domain packages are independent** - candidate doesn't import job, job doesn't import candidate
* **Bridge domains for relationships** - application domain connects candidate + job
* **Shared types in kernel** - not in individual domains
* **No circular dependencies** - enforced by Go
* **Each domain owns its errors** - `user.ErrUserNotFound()`, `job.ErrJobNotFound()`
* **Infrastructure in separate package** - `*infra/` suffix
* **Service layer in separate package** - `*srv/` suffix (avoids import cycles)

---

## 17. **Middleware: Composable Security Layers**

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

## 18. **Configuration: Environment-Driven**

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

## 19. **Error Handling Philosophy**

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

## 20. **Testing Strategy**

### What We Test:

1. **Domain logic** - Unit tests for entities
2. **Service layer** - Integration tests with mock repos
3. **API handlers** - E2E tests with test database
4. **Validation** - Edge cases for value objects

### Repository Mocks:

```go
// pkg/iam/user/usersrv/service_test.go
package usersrv_test

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

### The `AuthContext`:

```go
// pkg/kernel/auth_context.go
package kernel

type AuthContext struct {
    UserID      *UserID
    CandidateID *CandidateID  // For candidate-facing APIs
    TenantID    TenantID      // ← Always present
    Email       string
    Scopes      []string
    IsAPIKey    bool
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

## 22. **Security Principles**

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

## 23. **Observability: Logging Best Practices**

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

## 24. **Database Strategy**

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

## 25. **API Design Principles**

### RESTful Conventions:

```
POST   /api/jobs                      → Create job
GET    /api/jobs                      → List jobs
GET    /api/jobs/:id                  → Get one job
PUT    /api/jobs/:id                  → Update job
DELETE /api/jobs/:id                  → Delete job

POST   /api/jobs/:id/publish          → Actions as sub-resources

GET    /api/jobs/:job_id/applications → Get applications for job
POST   /api/jobs/:job_id/applications → Apply to job

GET    /api/candidates/me/applications → Candidate's applications
DELETE /api/applications/:id           → Withdraw application
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

## 26. **Code Style & Conventions**

### Naming:

* **Entities** - Singular nouns (`User`, `Tenant`, `Job`, `Application`)
* **Repositories** - `Repository` interface (`UserRepository`, `JobRepository`)
* **Services** - Service package with `*srv/` suffix (`usersrv`, `jobsrv`, `applicationsrv`)
* **Handlers** - `*Handlers` struct (`JobHandlers`, `CandidateHandlers`)
* **DTOs** - Suffixed with purpose (`CreateUserRequest`, `UserResponse`)

### File Organization:

```
user/
├── user.go          # Entity + domain methods + DTOs
├── repository.go    # Repository interface
├── errors.go        # Error registry
├── usersrv/         # Service layer (separate package to avoid cycles)
│   └── service.go
└── userinfra/       # Infrastructure implementations
    └── postgres.go
```

---

## 27. **What We Avoid**

### Anti-Patterns We Reject:

* ❌ **God objects** - No single struct that does everything
* ❌ **Anemic domain models** - Entities have behavior, not just getters/setters
* ❌ **Repository sprawl** - One repository per aggregate root
* ❌ **Service layer bypass** - Never call repos directly from handlers
* ❌ **DTO reuse** - Don't use same DTO for input and output
* ❌ **Null pointer exceptions** - Use pointer helper package
* ❌ **Primitive obsession** - Use value objects, not `string` everywhere
* ❌ **Magic strings** - Constants for error codes, scopes, etc.
* ❌ **Cross-domain imports** - Use bridge domains instead
* ❌ **Transactions everywhere** - Only when needed for multi-repo operations

---

## 28. **Performance Considerations**

### Optimization Strategy:

* **Eager loading** - Use `GetWithDetails()` to avoid N+1 queries
* **Batch fetching** - `GetByIDs()` for multiple entities
* **Pagination** - Never return unbounded lists
* **Caching** - At service layer for expensive operations
* **Connection pooling** - Database connections
* **Goroutines for async** - Non-blocking operations (email sending, etc.)

### Example:

```go
// ✅ Single batch fetch
jobs, err := s.jobRepo.GetByIDs(ctx, jobIDs)

// ❌ N+1 queries
applications := repo.List(ctx)
for _, app := range applications {
    job := jobRepo.GetByID(app.JobID)  // ← N queries!
}
```

---

## 29. **Documentation Standards**

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
* ✅ **Reliability** - Transactions ensure data consistency
* ✅ **Domain independence** - Change one domain without affecting others
* ✅ **Clear boundaries** - Bridge domains for relationships

**Good architecture makes the right thing easy and the wrong thing hard.**

This is that architecture.

---

*Version: 2.0*  
*Last Updated: 2025-12-07*

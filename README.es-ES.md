

# Manifiesto de Arquitectura: Construcción de Sistemas Multiinquilino Escalables

## 🚀 Uso como Esqueleto de Proyecto

Este repositorio sirve como un **esqueleto de proyecto en Go listo para producción** con patrones integrados para DDD, multiinquilino, autenticación y arquitectura escalable.

### Inicio Rápido

Utilice el **[Manifesto CLI](https://github.com/Abraxas-365/manifesto-cli)** para generar la estructura de un nuevo proyecto en segundos:

```bash
go install github.com/Abraxas-365/manifesto-cli/cmd/manifesto@latest
```

Requiere Go 1.23+.

**1. Cree un nuevo proyecto:**

```bash
# Interactivo — solo módulos principales
manifesto init myapp --module github.com/me/myapp

# Con módulos opcionales
manifesto init myapp --module github.com/me/myapp --with iam,fsx

# Todo incluido
manifesto init myapp --module github.com/me/myapp --all
```

**2. Agregue paquetes de dominio:**

```bash
cd myapp
manifesto add internal/recruitment/candidate
```

**3. Verifique la configuración:**

```bash
go mod tidy
go build ./...
```

> Para una lista completa de comandos CLI y módulos disponibles, consulte el [repositorio de Manifesto CLI](https://github.com/Abraxas-365/manifesto-cli).

---

## Filosofía y Principios Fundamentales

Este documento describe las decisiones arquitectónicas, patrones y principios que guían este proyecto. No son solo preferencias: representan lecciones aprendidas a duras penas sobre la construcción de sistemas empresariales mantenibles, escalables y seguros en cuanto a tipos.

---

## 1. **Diseño Guiado por el Dominio (DDD) como Base**

### ¿Por qué DDD?

El dominio empresarial es **complejo**, y el código debe **reflejar esa complejidad explícitamente** en lugar de ocultarla detrás de operaciones CRUD genéricas.

### Nuestra Implementación:

* **Entidades de Dominio Ricas** con comportamiento, no estructuras de datos anémicas
* **Objetos de Valor** para seguridad de tipos (`kernel.Email`, `kernel.DNI`, `kernel.JobID`)
* **Métodos de Dominio** que encapsulan reglas de negocio (`Tenant.CanAddUser()`, `User.HasScope()`)
* **Interfaces de Repositorio** que hablan el lenguaje del dominio

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

## 2. **Arquitectura Capa por Capa: Separación Clara de Responsabilidades**

### Las Capas:

```
┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
┃   Capa API (controladores, DTOs)     ┃  ← Controladores HTTP/Fiber
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Capa de Servicios (lógica negocio) ┃  ← Orquestación y flujos de trabajo
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Capa de Dominio (entidades, reglas)┃  ← Lógica central de negocio
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Capa de Repositorios (persistencia)┃  ← Contratos de acceso a datos
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃   Infraestructura (DB, S3, etc)      ┃  ← Detalles de implementación
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
```

### Reglas:

1. **Las dependencias fluyen solo hacia abajo** (sin dependencias cíclicas)
2. **La capa de dominio NO tiene dependencias externas** (Go puro)
3. **Las interfaces de repositorio viven en el dominio**, las implementaciones en infraestructura
4. **Los servicios orquestan**, las entidades aplican las reglas

---

## 3. **Seguridad de Tipos a Través de Objetos de Valor**

### El Paquete `internal/kernel`

En lugar de pasar `string` por todas partes, utilizamos **primitivas de dominio fuertemente tipadas**:

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

### Beneficios:

* **Seguridad en tiempo de compilación** — No se puede pasar accidentalmente un `UserID` donde se espera un `TenantID`
* **Código autodescriptivo** — `func GetUser(id kernel.UserID)` es más claro que `func GetUser(id string)`
* **Validación en un solo lugar** — `DNI.IsValid()` encapsula toda la lógica de validación
* **Refactorización fácil** — Cambie el tipo subyacente sin tocar todos los usos

---

## 4. **Patrón de Repositorio: Abstracción del Acceso a Datos**

### ¿Por qué Repositorios?

* **Probabilidad** — Simular repositorios en las pruebas
* **Flexibilidad** — Intercambiar PostgreSQL por MongoDB sin cambiar la lógica de negocio
* **Lenguaje de dominio** — `FindByEmail()` en lugar de `SELECT * FROM users WHERE...`

### Nuestra Convención:

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

**Nunca filtrar detalles de infraestructura** (consultas SQL, Mongo) a las capas de dominio/servicio.

---

## 5. **Independencia de Dominio: El Patrón de Relación Cruzada entre Dominios**

### El Problema: Evitar el Acoplamiento Fuerte

**❌ Enfoque Incorrecto:**

```go
// recruitment/candidate/candidate.go
package candidate

import "yourapp/recruitment/job"  // ❌ Creates tight coupling!

func (c *Candidate) GetAppliedJobs() ([]job.Job, error) {
    // This violates domain independence
}
```

### La Solución: Dominio Puente + Orquestación de Servicios

Cuando los dominios necesitan referenciarse entre sí (por ejemplo, candidatos aplicando a ofertas), utilizamos una **estrategia de tres niveles**:

#### Nivel 1: Dominio Puente (Dominio de Aplicación)

Cree un dominio separado que represente la **relación** entre entidades:

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

#### Nivel 2: Interfaz de Repositorio para Consultas de Relación

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

#### Nivel 3: La Capa de Servicios Orquesta la Lógica Cruzada entre Dominios

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

### Reglas de Independencia de Dominio:

| ✅ Permitido | ❌ Prohibido |
|:---|:---|
| El dominio importa `kernel` (tipos compartidos) | El dominio importa otro dominio |
| El servicio importa múltiples dominios | El repositorio importa el dominio |
| El dominio de aplicación hace referencia a IDs | El dominio de aplicación incrusta entidades |
| Los DTO combinan datos cruzados entre dominios | Las entidades tienen dependencias cruzadas entre dominios |
| El servicio orquesta lógica cruzada entre dominios | El controlador llama directamente a múltiples repositorios |

### Diagrama de Flujo de Dependencias:

```
┌─────────────────────────────────────────────────┐
│   Capa de Servicios (paquete applicationsrv)    │
│  ✅ Puede importar: application, candidate, job │
└────────────────────┬────────────────────────────┘
                     │ Orquesta
         ┌───────────┼───────────┐
         │           │           │
         ▼           ▼           ▼
┌────────────┐ ┌────────────┐ ┌────────────┐
│ application│ │  candidate │ │    job     │
│   dominio  │ │   dominio  │ │   dominio  │
└─────┬──────┘ └─────┬──────┘ └─────┬──────┘
      │              │              │
      └──────────────┴──────────────┘
                     │
                     ▼
              ┌────────────┐
              │   kernel   │ ← Primitivas compartidas
              │  (no deps) │
              └────────────┘
```

---

## 6. **Capa de Servicios: Orquestación y Coordinación**

### Responsabilidades del Servicio:

* **Coordinar múltiples repositorios**
* **Aplicar reglas de negocio entre entidades**
* **Gestionar transacciones cuando sea necesario** (ver sección 7)
* **Convertir entre DTOs y entidades de dominio**
* **Conectar múltiples dominios**

### Patrón de Ejemplo:

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

## 7. **Transacciones: Patrón Unidad de Trabajo (Unit of Work) (Cuando Sea Necesario)**

### ⚠️ No Cada Servicio Necesita Esto

El patrón Unidad de Trabajo (UoW) existe para resolver un problema específico: **garantizar la atomicidad en escrituras a múltiples repositorios**. La mayoría de los servicios simples —aquellos que solo leen datos o solo escriben en un solo repositorio— **no necesitan un UoW**. Agregarlo por todas partes introduce complejidad innecesaria.

> **Regla general:** Inyecte `kernel.UnitOfWork` en un servicio solo si ese servicio tiene al menos una operación que **escriba en dos o más repositorios** y deba tener éxito o fallar como una unidad.

### El Problema: Operaciones Multi-Repositorio

```go
// ❌ PROBLEM: What if step 2 fails? Step 1 is already committed!
func (s *UserService) CreateUser(ctx context.Context, req CreateUserRequest) error {
    userRepo.Create(ctx, user)      // Step 1 ✅
    tenantRepo.UpdateCount(ctx, t)  // Step 2 ❌ FAILS — user was already saved!
    roleRepo.Assign(ctx, role)      // Step 3 never runs
}
```

### La Solución: Interfaz de Unidad de Trabajo

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

### Implementación de Infraestructura

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

### Soporte de Transacciones en Repositorios

Cualquier repositorio que pueda participar en una transacción debe soportar tanto contextos transaccionales como no transaccionales a través de una ayuda `getExecutor`:

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

**Aplique este patrón a TODOS los repositorios** — no tiene costo y les permite participar en transacciones cuando sea necesario sin cambiar su interfaz.

### Servicio con UoW — Solo Cuando esté Justificado

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

### Cuándo Usar UoW — Guía de Decisión:

| Escenario | ¿Usar UoW? | Patrón |
|:---|:---:|:---|
| Operaciones de solo lectura | ❌ No | Llamada directa al repositorio |
| Escritura en un solo repositorio | ❌ No | Llamada directa al repositorio |
| Escritura + lectura (misma entidad) | ❌ No | Un solo repositorio lo maneja |
| Escrituras en múltiples repositorios | ✅ Sí | `kernel.WithTransaction` |
| API externa + escritura en BD | ✅ Sí | Transacciones compensatorias |

---

## 8. **DTOs: Transformación de Entrada/Salida**

### ¿Por qué DTOs?

* **Control de versiones de API** — Cambiar DTOs sin modificar entidades de dominio
* **Seguridad** — No exponer IDs internos ni campos sensibles
* **Validación en los límites** — Validar la entrada antes de entrar al dominio
* **Separación** — Entidades de dominio ≠ Respuestas de API

### Nuestro Patrón:

```go
// Input DTO
type CreateCandidateRequest struct {
    Email     kernel.Email     `json:"email" validate:"required,email"`
    FirstName kernel.FirstName `json:"first_name" validate:"required"`
    LastName  kernel.LastName  `json:"last_name" validate:"required"`
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
    ID           kernel.CandidateID
    TenantID     kernel.TenantID
    Email        kernel.Email
    FirstName    kernel.FirstName
    LastName     kernel.LastName
    CreatedAt    time.Time
    UpdatedAt    time.Time
    PasswordHash string  // Never exposed
    IsActive     bool
}
```

---

## 9. **Manejo de Errores: Errores Ricos y Estructurados**

### El Paquete `internal/errx`

Rechazamos el `error` genérico a favor de **tipos de error ricos** con contexto:

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

    CodeJobNotPublished = ErrRegistry.Register(
        "JOB_NOT_PUBLISHED",
        errx.TypeBusiness,
        http.StatusForbidden,
        "Job is not published",
    )
)

func ErrJobNotFound() *errx.Error       { return errx.New(CodeJobNotFound) }
func ErrJobNotPublished() *errx.Error   { return errx.New(CodeJobNotPublished) }

func ErrJobNotFoundWithID(jobID kernel.JobID) *errx.Error {
    return ErrJobNotFound().WithDetail("job_id", jobID)
}
```

### Beneficios:

* **Errores tipados** — `errx.Type` categoriza errores (Validación, Negocio, Interno)
* **Códigos de estado HTTP** — Mapeo automático a respuestas HTTP correctas
* **Contexto estructurado** — `WithDetail()` agrega información de depuración
* **Encapsulamiento** — Preservar cadenas de errores con `errx.Wrap()`

---

## 10. **Multiinquilino (Multi-Tenancy): Preocupación de Primer Nivel**

* **Cada entidad** tiene un `TenantID`
* **Todas las consultas** filtran por inquilino
* **AuthContext** transporta `TenantID` a lo largo del ciclo de vida de la solicitud
* **Repositorios** aplican límites de inquilino

```go
// ✅ Always scoped to tenant
func (r *Repository) FindByID(ctx context.Context, id UserID, tenantID TenantID) (*User, error)

// ❌ Never global lookups
func (r *Repository) FindByID(ctx context.Context, id UserID) (*User, error)
```

---

## 11. **Ámbitos (Scopes) y Roles: Control de Acceso Finamente Granular**

### Ámbitos (Scopes)

Los ámbitos son la unidad atómica de permiso. Siguen un patrón `recurso:acción` y pueden asignarse directamente a usuarios o agruparse en roles.

* **Composabilidad** — Combinar permisos
* **Amigable con API** — Funciona tanto para usuarios como para claves API
* **Compatible con OAuth** — Patrón estándar
* **Soporte para comodines** — `jobs:*` coincide con todos los permisos de ofertas

```go
const (
    ScopeJobsRead    = "jobs:read"
    ScopeJobsWrite   = "jobs:write"
    ScopeJobsAll     = "jobs:*"
)
```

### Roles

Los roles son colecciones nombradas de ámbitos que pueden asignarse a usuarios. Los permisos efectivos de un usuario son la unión de sus ámbitos directos y todos los ámbitos de sus roles asignados.

```
Usuario "alice"
├── Ámbitos directos: ["reports:view"]
└── Roles:
    ├── "Editor"   → ["content:read", "content:write"]
    └── "Reviewer" → ["content:read", "reviews:write"]

Ámbitos efectivos: ["reports:view", "content:read", "content:write", "reviews:write"]
```

### Requisitos de Ámbitos para la Gestión de Roles y Ámbitos

Cada punto final de gestión está protegido por ámbitos:

**Puntos finales de roles** (`/roles`):

| Ámbito Requerido | Puntos Finales |
|:---|:---|
| `roles:read` | `GET /roles`, `GET /roles/:id`, `GET /users/:userId/roles` |
| `roles:write` | `POST /roles`, `PUT /roles/:id` |
| `roles:delete` | `DELETE /roles/:id` |
| `roles:assign` | `POST /roles/:id/assign`, `DELETE /roles/:id/users/:userId` |

**Puntos finales de ámbitos** (`/scopes`, `/users/:userId/scopes`):

| Ámbito Requerido | Puntos Finales |
|:---|:---|
| `scopes:read` | `GET /scopes`, `GET /users/:userId/scopes` |
| `scopes:write` | `PUT /users/:userId/scopes` |
| `scopes:assign` | `POST /users/:userId/scopes`, `DELETE /users/:userId/scopes` |

### Uso de Middleware

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

## 12. **Autenticación: OAuth + JWT + Claves API**

### Estrategia de Autenticación Unificada:

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

### Flujo OAuth:

1. **Requiere invitación** — Sin registro propio (modelo B2B SaaS)
2. **Gestión de estado** — Protección CSRF mediante tokens de estado
3. **Abstracción de proveedor** — Google, Microsoft detrás de la interfaz `OAuthService`
4. **Generación de tokens** — JWT internos tras éxito en OAuth

---

## 13. **Paquetes Reutilizables: Construir una Vez, Usar en Todas Partes**

### `internal/errx` — Manejo de Errores
* Creación de errores seguros en tipos, mapeo de estado HTTP, registros de errores por módulo

### `internal/logx` — Registro (Logging)
* Salida de consola coloreada inspirada en Rust, formateadores JSON/CloudWatch, registro estructurado

### `internal/fsx` — Abstracción del Sistema de Archivos
* Basado en interfaces (funciona con S3, FS local), operaciones conscientes del contexto

### `internal/ptrx` — Utilidades de Punteros
* `Value[T]` y `ValueOr[T]` genéricos, campos anulables seguros en tipos

### `internal/kernel` — Primitivas de Dominio
* Objetos de valor compartidos (`UserID`, `TenantID`), `AuthContext`, `Paginated[T]`, y opcionalmente `UnitOfWork` para servicios que necesiten transacciones

---

## 14. **Paginación: Consistente y Segura en Tipos**

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

## 15. **Inyección de Dependencias: Explícita y Probable**

### Inyección por Constructor:

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

### Sin Magia:
* **Sin DI basado en reflexión**
* **Sin localizadores de servicios**
* **Ensamblaje explícito** en `main.go` o contenedor DI
* **Fácil de probar** — Solo pasar mocks

---

## 16. **Organización de Paquetes: Centrada en el Dominio**

```
internal/
├── kernel/           # Primitivas de dominio compartidas
├── errx/             # Marco de manejo de errores
├── logx/             # Marco de registro
├── fsx/              # Abstracción del sistema de archivos
├── ptrx/             # Utilidades de punteros
└── iam/              # Gestión de Identidad y Acceso
    ├── user/
    │   ├── user.go
    │   ├── repository.go
    │   ├── usersrv/
    │   │   └── service.go
    │   └── userinfra/
    │       └── postgres.go
    ├── tenant/
    ├── role/
    ├── invitation/
    ├── apikey/
    ├── iaminfra/     # Infra compartida (UoW — solo si IAM lo necesita)
    │   └── uow.go
    └── auth/

recruitment/
├── candidate/
│   ├── candidate.go
│   ├── repository.go
│   ├── errors.go
│   ├── candidatesrv/
│   └── candidateinfra/
├── job/
└── application/      # Dominio puente (relaciones)
    ├── application.go
    ├── repository.go
    ├── dtos.go
    ├── errors.go
    ├── applicationsrv/
    └── applicationinfra/
```

### Principios:
* **Los paquetes de dominio son independientes** — `candidate` no importa `job`
* **Dominios puente para relaciones** — `application` conecta candidato + oferta
* **Tipos compartidos en kernel** — no en dominios individuales
* **Sin dependencias circulares**
* **Infraestructura en `*infra/`**, capa de servicios en `*srv/`

---

## 17. **Middleware: Capas de Seguridad Componibles**

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

## 18. **Configuración: Impulsada por Entorno**

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

## 19. **Filosofía de Manejo de Errores**

1. **Los errores son datos** — Estructurelos correctamente
2. **El contexto importa** — Use `WithDetail()` con liberalidad
3. **Errores tipados** — `TypeValidation` vs `TypeBusiness` vs `TypeInternal`
4. **Encapsule, no oculte** — Preserve las cadenas de errores
5. **Conscientes de HTTP** — Los errores conocen sus códigos de estado HTTP

```go
// ✅ Rich error with context
return s3Errors.NewWithCause(ErrFailedUpload, err).
    WithDetail("path", path).
    WithDetail("bucket", fs.bucket)

// ❌ Generic error
return fmt.Errorf("upload failed: %w", err)
```

---

## 20. **Estrategia de Pruebas**

1. **Lógica de dominio** — Pruebas unitarias para entidades
2. **Capa de servicios** — Pruebas de integración con repositorios simulados
3. **Controladores de API** — Pruebas E2E con base de datos de pruebas
4. **Validación** — Casos límite para objetos de valor

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

## 21. **Propagación de Contexto: Ciclo de Vida de la Solicitud**

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

## 22. **Principios de Seguridad**

1. **Autenticación por middleware** — Validar antes de llegar a los controladores
2. **Aplicación de ámbitos** — Permisos finamente granulares
3. **Aislamiento de inquilino** — Cada consulta filtrada por `TenantID`
4. **Validación de entrada** — DTOs con etiquetas `validate`
5. **Encriptación de claves API** — Nunca almacenar secretos en texto plano
6. **Expiración de tokens** — JWT de corta vida (15 min), tokens de refresco (7 días)
7. **Registro solo por invitación** — Sin registro propio para B2B SaaS

---

## 23. **Observabilidad: Mejores Prácticas de Registro**

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

## 24. **Estrategia de Base de Datos**

* **Migraciones controladas por versión** en `/migrations`
* **Idempotentes** — pueden ejecutarse múltiples veces de forma segura
* **Soporte para retroceso** — siempre se proporcionan migraciones descendentes
* **Sentencias preparadas** — prevenir inyección SQL
* **Operaciones por lotes** — inserciones/actualizaciones masivas cuando sea posible
* **Eliminaciones suaves** — `deleted_at` para registros de auditoría

---

## 25. **Principios de Diseño de API**

```
POST   /api/jobs                       → Crear oferta
GET    /api/jobs                       → Listar ofertas
GET    /api/jobs/:id                   → Obtener una oferta
PUT    /api/jobs/:id                   → Actualizar oferta
DELETE /api/jobs/:id                   → Eliminar oferta
POST   /api/jobs/:id/publish           → Acciones como sub-recursos
GET    /api/jobs/:job_id/applications  → Solicitudes para la oferta
GET    /api/candidates/me/applications → Propias solicitudes del candidato
DELETE /api/applications/:id           → Retirar solicitud
```

---

## 26. **Estilo de Código y Convenciones**

* **Entidades** — Sustantivos en singular (`User`, `Tenant`, `Job`)
* **Repositorios** — Interfaz `Repository` por dominio
* **Servicios** — Sufijo de paquete `*srv/` (`usersrv`, `jobsrv`)
* **Controladores** — Estructura `*Handlers` (`JobHandlers`)
* **DTOs** — Con sufijo según propósito (`CreateUserRequest`, `UserResponse`)

---

## 27. **Lo que Evitamos**

* ❌ **Objetos Dios** — Ninguna estructura única que haga todo
* ❌ **Modelos de dominio anémicos** — Las entidades tienen comportamiento
* ❌ **Omisión de la capa de servicios** — Nunca llamar a repositorios directamente desde controladores
* ❌ **Reutilización de DTOs** — No usar el mismo DTO para entrada y salida
* ❌ **Obsesión por primitivos** — Use objetos de valor, no `string` por todas partes
* ❌ **Cadenas mágicas** — Constantes para códigos de error, ámbitos, etc.
* ❌ **Importaciones cruzadas entre dominios** — Use dominios puente en su lugar
* ❌ **UoW por todas partes** — Inyecte `UnitOfWork` solo donde realmente se requiera atomicidad multi-repositorio

---

## 28. **Consideraciones de Rendimiento**

* **Carga ávida (Eager loading)** — Use `GetWithDetails()` para evitar consultas N+1
* **Obtención por lotes** — `GetByIDs()` para múltiples entidades
* **Paginación** — Nunca devolver listas sin límite
* **Almacenamiento en pool de conexiones** — Conexiones a base de datos
* **Gorutinas para asíncrono** — Operaciones no bloqueantes (correo, notificaciones)

```go
// ✅ Single batch fetch
jobs, err := s.jobRepo.GetByIDs(ctx, jobIDs)

// ❌ N+1 queries
for _, app := range applications {
    job := jobRepo.GetByID(app.JobID)  // ← N queries!
}
```

---

## Conclusión: La Arquitectura como Producto

Cada decisión aquí sirve **objetivos específicos**:

* ✅ **Mantenibilidad** — Los nuevos desarrolladores pueden navegar la base de código
* ✅ **Probabilidad** — Interfaz de simulación (mock), no implementaciones
* ✅ **Escalabilidad** — Multiinquilino desde el día uno
* ✅ **Seguridad** — Defensa en profundidad, permisos basados en ámbitos
* ✅ **Seguridad de tipos** — Capturar errores en tiempo de compilación
* ✅ **Flexibilidad** — Intercambiar implementaciones sin cambiar contratos
* ✅ **Simplicidad** — Sin abstracciones innecesarias; UoW solo cuando se requiere atomicidad
* ✅ **Fiabilidad** — Transacciones donde la consistencia de datos lo exige
* ✅ **Independencia de dominio** — Cambiar un dominio sin afectar a otros

**Una buena arquitectura hace que lo correcto sea fácil y lo incorrecto, difícil.**

---

*Versión: 2.1*
*Última Actualización: 2026-03-06*

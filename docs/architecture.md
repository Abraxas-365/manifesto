# Architecture

[Documentation index](README.md)

## Organize around business capabilities

A domain module owns its entities, contracts, use cases, and adapters. IAM is a bounded context containing related modules such as users, roles, tenants, and invitations. Infrastructure utilities are separate packages rather than business modules disguised as global services.

```text
cmd/                        Application entry point and root composition
internal/kernel/            Shared identity and request primitives
internal/iam/                Identity and access bounded context
internal/<module>/           Other capabilities and utility packages
migrations/                  PostgreSQL schema evolution
examples/                    Executable usage examples
```

## Ports and adapters

| Layer | Responsibility | Must not become |
| --- | --- | --- |
| Domain | Entities, value types, DTOs, validation, errors, required interfaces | A wrapper around database rows with no business meaning |
| Service (`*srv`) | Use-case orchestration, authorization policies, repository calls | An HTTP handler that requires Fiber context |
| Infrastructure (`*infra`) | SQL, external clients, concrete port implementations | An implicit service locator |
| API (`*api`) | Bind requests, establish transport context, call services, return responses | The only place business invariants are enforced |

Dependencies point toward domain contracts. Services depend on repository interfaces; infrastructure implements them. Go package boundaries enforce dependencies, not transaction or authorization correctness by themselves.

**Current exceptions:** authentication orchestration still lives partly in `auth` handlers, and invitation acceptance combines workflow checks and transactional persistence in `authinfra`. Not every utility or AI package uses the business-module directory pattern. These are implementation facts, not examples of strict layering to reproduce unquestioningly.

## Explicit composition

The [root container](../cmd/container.go) constructs shared PostgreSQL, Redis, and file-storage dependencies. The [IAM container](../internal/iam/iamcontainer/container.go) receives dependencies through a `Deps` struct and constructs repositories, services, handlers, and middleware.

Use ordinary constructors. Do not discover services by reflection, hide repositories in context values, or make a handler assemble its own infrastructure. Configuration determines which adapters are wired; a configuration field alone does not implement an integration.

## Shared types and module boundaries

[Kernel](../internal/kernel) contains shared types without importing business modules. Use typed entity IDs such as `kernel.UserID` and `kernel.TenantID` to prevent accidental interchange. Convert URL parameters at the transport boundary.

```go
userID := kernel.NewUserID(c.Params("id"))
```

A typed string is not UUID validation or authorization. Validate the input as required and constrain repository operations by tenant. Prefer ID references and narrow ports to coupling unrelated domains through full entities. Closely related modules inside a bounded context may collaborate explicitly.

## Validation at every entry point

Handlers use `kernel.BindAndValidate[T]`. Public service methods accepting request DTOs also call `Validate()` before repository work. Where a service accepts individual fields, it can assemble the corresponding existing DTO to reuse validation without changing its public signature.

```go
if err := req.Validate(); err != nil {
    return nil, err
}
```

Keep validators deterministic and free of database/network access or mutation. Calling the same validator in the handler and service is intentional: jobs and internal callers do not pass through HTTP.

Separate responsibilities:

- **DTO:** shape, required fields, supported values.
- **Service/domain:** caller authority, lifecycle eligibility, business rules.
- **Persistence:** uniqueness, atomicity, concurrent-write integrity.

Do not invent transport DTOs for unrelated low-level utilities merely to make everything expose `Validate()`.

## Authorization at the service boundary

Customer IAM services accept `*kernel.AuthContext` explicitly and use `authz.Require` before repository work. The helper checks identity, exact tenant ownership, and a scope constant; `*` never skips ownership. Grant coverage and actor-kind restrictions remain separate service rules. Middleware rejects unauthorized HTTP requests early, while tenant predicates constrain persistence.

`module services → iam/authz → kernel + IAM errors` keeps this policy independent of authentication adapters, HTTP, and databases. Do not hide required authority in ambient context values or accept it from request bodies. Trusted authentication/bootstrap paths remain separate. See [ADR 005](decisions/005-explicit-service-authorization.md).

## Errors are part of the contract

Declare stable domain error codes through `errx.Registry`. Return domain errors from services and translate infrastructure errors at the adapter boundary. A missing SQL row should become a domain not-found error, not escape as `sql.ErrNoRows`.

```go
existing, err := repo.GetByName(ctx, name, tenantID)
if err != nil && !errors.Is(err, ErrNotFound()) {
    return err
}
if existing != nil {
    return ErrAlreadyExists()
}
```

This is a schematic pattern; use the actual module's interface and error names. Compare errors using `errors.Is` or the relevant `errx` helper. Do not silently treat a database outage as absence. If an operation is deliberately best-effort, document that choice and make failure observable.

## Naming convention

Use the same vocabulary across layers for new code:

| Prefix | Intended result |
| --- | --- |
| `Get` | One entity |
| `List` | A collection without pagination |
| `Find` | A filtered or paginated result |

Existing IAM ports still include names such as `FindByID` and `FindByTenant` that predate this convention. Follow their actual signatures when integrating; a naming cleanup should be an explicit change, not hidden inside a feature.

## Adding a capability

1. Define entities, typed IDs, request DTOs, domain errors, and required ports.
2. Implement use cases in `*srv`, including validation and permission checks.
3. Implement persistence/external adapters in `*infra`.
4. Add handlers in `*api`, using typed IDs and authenticated tenant context.
5. Wire dependencies in the appropriate container.
6. Register routes in [cmd/server.go](../cmd/server.go), using scope constants rather than literal permission strings.
7. Test service behavior and adapter invariants separately; exercise transactions against PostgreSQL.
8. Update the guides and add a decision record when changing a boundary or policy.

See [architecture decisions](decisions/README.md) for why these conventions were chosen.

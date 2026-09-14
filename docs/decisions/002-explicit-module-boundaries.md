# 002 — Explicit module boundaries

**Status:** Accepted convention; existing exceptions documented

## Context

Business rules must remain usable from HTTP, jobs, and internal callers. Database-specific code and dependency discovery can make those rules hard to test, change, and locate. Validation only in routes leaves internal callers able to bypass the same input contract.

## Decision

- Organize business capabilities into domain, service, infrastructure, and API packages.
- Define required ports alongside the domain; infrastructure implements those contracts.
- Assemble dependencies with ordinary constructors and explicit composition roots.
- Represent entity IDs with typed wrappers in `kernel`.
- Validate request DTOs both at the HTTP boundary and at public service entry points, before repository work.
- Keep permission and lifecycle decisions separate from shape validation.
- Translate infrastructure failures to stable domain errors and compare errors by code using `errors.Is`/`errx` helpers.

The extra `Validate()` call is intentional. Validators are cheap, deterministic checks, not calls to remote systems. Database constraints and transactions still enforce persistence integrity.

## Alternatives

- **Handlers calling SQL directly:** initially smaller, but couples transport and business behavior.
- **Reflection-driven dependency injection:** reduces constructor boilerplate at the cost of less visible wiring and harder startup diagnosis.
- **Transport-only validation:** avoids duplicate calls but trusts every non-HTTP caller to remember the rules.
- **Raw string IDs everywhere:** convenient, but allows accidental mixing of distinct entity identifiers.

## Consequences

- Dependencies, validation, and failure contracts are visible in source.
- Services can be invoked without fabricating an HTTP request.
- Constructors and narrow interfaces introduce intentional boilerplate.
- Typed IDs prevent type confusion, not malformed values or unauthorized access.
- Repository implementations must preserve behavior beyond their method signatures.
- The pattern is a target, not a claim of perfect separation today: auth handlers retain orchestration, `authinfra` owns transactional onboarding checks, and some older repository names differ from the current naming convention.

See [architecture](../architecture.md) for the practical conventions.

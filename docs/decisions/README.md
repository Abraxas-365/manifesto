# Architecture decision records

[Documentation index](../README.md)

These records capture the rationale behind Manifesto's current direction. They are retrospective explanations of the implementation and project conventions, not a claim that every module already conforms perfectly.

| Record | Decision |
| --- | --- |
| [001 — Application-owned source](001-application-owned-source.md) | Reuse by copying and adapting rather than importing a framework runtime |
| [002 — Explicit module boundaries](002-explicit-module-boundaries.md) | Ports and adapters, constructors, typed IDs, validation and error contracts |
| [003 — Tenant identity and lifecycle](003-tenant-identity-and-lifecycle.md) | Separate tenant authority from platform operations; model actors and workflows explicitly |
| [004 — Persistence and credential invalidation](004-persistence-and-credential-invalidation.md) | Explicit transactions and persistent credential generations |
| [005 — Explicit service authorization](005-explicit-service-authorization.md) | Trusted contexts, tenant/scope checks, and grant coverage inside customer services |

## Adding a decision

Use the next sequence number and record:

1. **Status:** accepted, proposed, or superseded.
2. **Context:** the problem and constraints.
3. **Decision:** the chosen approach.
4. **Alternatives:** meaningful options that were not selected.
5. **Consequences:** benefits, costs, operational requirements, and current exceptions.

Do not rewrite an old record to disguise a change in direction. Add a replacement and link the superseded decision. Routine implementation details belong in the guides, not necessarily in a new record.

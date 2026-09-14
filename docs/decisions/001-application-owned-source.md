# 001 — Application-owned source

**Status:** Accepted direction

## Context

A backend foundation can remove repetitive setup, but runtime frameworks can make ordinary business changes depend on extension hooks, framework releases, and hidden conventions. Multi-tenant applications also differ in deployment, business policy, and integrations.

## Decision

Provide a Go reference implementation and source modules that a consuming application owns and adapts. The application's `internal/` tree is its implementation, not a public library API imported from Manifesto at runtime.

Project-generation tooling is separate from the reference server. The [Manifesto CLI](https://github.com/Abraxas-365/manifesto-cli) has its own repository and command contract. This checkout contains a basic clone-and-rewrite script, not the CLI itself.

## Alternatives

- **Importable framework runtime:** easier centralized upgrades, but pushes application variation into extension APIs and framework compatibility constraints.
- **Architecture prose only:** communicates ideas, but leaves each team to implement and reconcile all integrations independently.
- **Copy without conventions:** offers ownership but loses a shared structure for understanding and maintaining the resulting application.

## Consequences

- Teams can change internals directly and keep code aligned with their domain.
- The implementation acts as an executable reference rather than a promise that every application should preserve every package.
- Fixes do not automatically propagate to generated projects. Consumers must track relevant upstream changes and review security and dependency updates.
- Source ownership increases responsibility: a copied implementation is not automatically production-ready, supported, or independently audited.

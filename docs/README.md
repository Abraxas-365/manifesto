# Documentation

Manifesto documents both its implementation and the reasoning behind it. Guides describe current behavior; decision records describe the chosen direction and its trade-offs. A convention is not a guarantee that every older package already follows it.

## Reading paths

**Evaluating Manifesto**

1. [Project overview](../README.md)
2. [Architecture](architecture.md)
3. [Architecture decisions](decisions/README.md)
4. [Operations and limitations](operations.md)

**Building an application**

1. [Local development](development.md)
2. [Module guide](modules.md)
3. [Architecture and extension conventions](architecture.md)
4. [IAM](iam.md)

**Changing authentication or deploying**

1. [IAM](iam.md)
2. [Identity and lifecycle decisions](decisions/003-tenant-identity-and-lifecycle.md)
3. [Persistence and invalidation decisions](decisions/004-persistence-and-credential-invalidation.md)
4. [Operations and migrations](operations.md)

## Maintaining this reference

- Update behavior documentation when changing routes, request contracts, authorization, or migrations.
- Record consequential design changes with context, decision, alternatives, and consequences.
- Link to source instead of copying large implementations or environment-variable inventories.
- Label intended architecture separately from existing exceptions and unimplemented capabilities.
- Never describe a build or unit-test pass as proof of production readiness.

# Module guide

[Documentation index](README.md)

Modules are application-owned source under `internal/`. Availability in this repository does not imply every module is wired into the reference HTTP server.

| Package | Responsibility | Integration notes |
| --- | --- | --- |
| [kernel](../internal/kernel) | Typed IDs, actor/auth context, scope matching, request binding, generic store port | Shared foundation; no business-module dependencies |
| [errx](../internal/errx) | Structured errors, registries, codes, wrapping and comparison | Domain errors cross layers; HTTP mapping happens at the boundary |
| [config](../internal/config) | Environment-based application configuration | Inspect loaders and the Makefile for actual defaults |
| [logx](../internal/logx) | Structured application logging | Logging is not a durable audit database |
| [iam](../internal/iam) | Tenants, membership, roles, scopes, invitations, authentication, API keys | Pre-wired in the reference server; see [IAM](iam.md) |
| [asyncx](../internal/asyncx) | Concurrent maps, pools, batches, debounce, pipelines and streams | In-process execution, not durable background jobs |
| [jobx](../internal/jobx) | Redis-backed background jobs, queues, retries and delayed execution | Requires application-specific worker/handler wiring |
| [fsx](../internal/fsx) | File-system port with local and S3 adapters | Root container selects local or S3 storage |
| [notifx](../internal/notifx) | Email abstraction, console and SES implementations, templates | IAM notification ports need adapters; they are not automatically wired to SES |
| [ptrx](../internal/ptrx) | Pointer and optional-value helpers | Small helpers rather than a replacement for explicit domain types |
| [ai](../internal/ai) | AI provider integrations and agent harness | Separate from IAM; provider configuration and credentials depend on the example/adapter |

## Examples

Start with [examples](../examples) for concurrency, storage, and AI usage. The [AI harness examples](../internal/ai/harness/examples) provide additional focused integrations. Inspect an example's source and requirements before running it: external services, API credentials, or filesystem access may be involved.

## Adapter ownership

A port names a capability the application needs. An adapter binds that capability to a specific dependency. Keep that translation visible:

- IAM depends on its own OTP/invitation notification interfaces, not an assumed global mail service.
- PostgreSQL adapters use SQL and `sqlx`; switching databases requires preserving transaction and concurrency semantics, not merely implementing matching method signatures.
- A Redis-backed queue and an in-process concurrency pool solve different durability problems.
- Source-level reuse means your application owns dependency upgrades and review of upstream changes.

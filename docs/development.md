# Local development

[Documentation index](README.md)

## Requirements

- Go **1.25.4 or newer**; see [go.mod](../go.mod).
- Docker with the Compose v2 plugin (`docker compose`).
- Make and a POSIX-compatible shell for the commands below.

The reference server requires both PostgreSQL and Redis. The supplied [Compose file](../docker-compose.yml) uses PostgreSQL 15 and Redis 7 and persists their data in named volumes.

## 1. Obtain and check the code

```bash
git clone https://github.com/Abraxas-365/manifesto.git
cd manifesto
go mod download
go build ./...
go test ./...
```

These checks do not establish that migrations or database concurrency behavior work correctly.

## 2. Start local dependencies

The checked-in Compose command passes `--requirepass` with no argument when the default Redis password is empty. For local development, use a nonempty password consistently for startup and the application:

```bash
make up REDIS_PASSWORD=manifesto-local-only
docker exec manifesto-postgres pg_isready -U manifesto -d manifestodb
docker exec -e REDISCLI_AUTH=manifesto-local-only manifesto-redis redis-cli ping
```

Wait for PostgreSQL readiness and a Redis `PONG`. The existing Compose Redis health probe is unauthenticated, so do not rely on it when using a password. Inspect `make postgres-logs` and `make redis-logs` if startup fails. The Compose services publish local ports; the password above is a disposable development example, not a production secret.

## 3. Initialize a fresh database

**Current blocker:** migration `003` declares `refresh_tokens.session_id` as `UUID`, but migration `001` defines the referenced `user_sessions.id` as `VARCHAR(255)`. PostgreSQL cannot create that foreign key. Resolve this schema mismatch in a reviewed migration change before attempting a complete fresh installation. The documentation rewrite does not modify application SQL.

The following is the initialization procedure **after that incompatibility is fixed**, not a currently working end-to-end quickstart.

**Only run this against a fresh, disposable local database.** These files are not idempotent, and this loop does not track migration history. The command uses the default Compose container/database/user names.

```bash
(
  set -eu
  for migration in migrations/*.up.sql; do
    printf 'Applying %s\n' "$migration"
    docker exec -i manifesto-postgres \
      psql -X -v ON_ERROR_STOP=1 --single-transaction \
      -U manifesto -d manifestodb < "$migration"
  done
)
```

Each file executes in a transaction and stops on SQL errors. If a later file fails, earlier files remain applied: inspect the failure and resume deliberately rather than rerunning the whole loop.

For an existing database, use a migration tool or deployment procedure that records applied versions. Review the [migration notes](operations.md) before upgrading.

**Makefile caveat:** `make migrate` currently references `migrations/001_genesis.sql`, which is not the actual filename. Do not use `make init`, `make setup`, or `make db-reset` as a substitute for the explicit migration step above. This checkout also has no seed SQL file; `make seed` does not provision a first administrator.

## 4. Run the reference server

After resolving the schema blocker and applying the migrations:

```bash
make dev REDIS_PASSWORD=manifesto-local-only
```

The Makefile exports environment variables and runs `go mod tidy` followed by `go run ./cmd`. A separate `.env` file is not required for this path. Directly running the binary requires you to supply the necessary environment yourself.

In another terminal:

```bash
curl --fail http://localhost:8080/health
```

A healthy response reports PostgreSQL and Redis connectivity. It does not prove authentication setup, migration completeness, or email delivery.

## 5. Configure an authentication path

An empty database can serve health requests but does not contain a tenant or administrator. Customer onboarding is invitation-based; it does not provide unauthenticated tenant creation or a public `POST /users` endpoint.

Provision the initial tenant and administrator through a trusted application-specific bootstrap process. This repository does not currently include a working seed or a complete public registration/bootstrap flow. Do not bypass invitation verification or expose cross-tenant admin handlers just to make signup work.

OAuth providers are disabled by default. Configure the enabled provider's credentials and callback URL, or use the passwordless flow for an existing OTP-enabled member. The reference container prints OTPs and development invitation tokens to logs instead of sending email. See [IAM](iam.md).

## Configuration

The [Makefile](../Makefile) is the local configuration entry point; [internal/config](../internal/config) defines what the application reads. Override a value for a command:

```bash
make dev SERVER_PORT=9090 REDIS_PASSWORD=manifesto-local-only
```

Main groups include server/CORS, PostgreSQL, Redis, JWT/session/cookie settings, OAuth, OTP/invitations, tenant defaults, and storage. Keep production secrets out of committed files and shell history. `make env` prints configuration values and should not be used in public CI logs with secrets loaded.

A declared email-provider setting does not replace the console IAM notifiers: change the explicit wiring in the root container.

## Useful checks

```bash
go test ./...
go test -race ./internal/iam/...
go vet ./...
git diff --check
```

`make build` writes `bin/server`; `make dev-watch` needs `air`; linting needs `golangci-lint`. Inspect the Makefile before using destructive helpers such as `down-v` or `db-reset`.

## Starting a separate project

The [Manifesto CLI](https://github.com/Abraxas-365/manifesto-cli) is maintained separately. Consult that repository for its current installation and module-generation contract. The local [init-project.sh](../init-project.sh) is a simpler clone-and-rewrite helper, not the CLI implementation.

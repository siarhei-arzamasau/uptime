# Go authentication with PostgreSQL, GORM, and JWT

## ORM selection and architecture

Use **GORM** with `gorm.io/driver/postgres`. It fits our user and session models: the PostgreSQL driver uses pgx and supports transactions and `FOR UPDATE` locks needed for safe token rotation. [PostgreSQL driver](https://gorm.io/docs/connecting_to_the_database.html), [transactions](https://gorm.io/docs/transactions.html), [locking](https://gorm.io/docs/advanced_query.html).

- HTTP server: standard `net/http`; JWT: `golang-jwt/jwt/v5`.
- Keep HTTP controllers in their owning modules: authentication in `internal/auth/`. Shared HTTP middleware and JSON responses belong in `internal/httpx/`; compose module routes in `cmd/api/`.
- Use GORM inside repositories; never return database models directly over HTTP.
- Use one `database/sql` pool managed through GORM; do not create a separate `pgxpool`.
- Change the schema through versioned SQL migrations with `goose`, using a separate Go command. Do not run `AutoMigrate` on application startup.
- Pin dependencies in `go.mod` and `go.sum`.

## PostgreSQL and Docker

- Check Docker and Compose; start the installed Docker Desktop and wait for readiness. If Docker is missing, report the installation requirement.
- Add `backend/docker-compose.yml`: PostgreSQL 17, a named volume, a `pg_isready` healthcheck, and port `127.0.0.1:5432`.
- Read database settings from local `.env`; add `.env.example` without real secrets.
- Run `docker compose up -d --wait`, check connectivity, and apply migrations.
- Document startup, logs, and `docker compose down`, which preserves data. Document volume deletion separately as removal of the local database.
- Run the Go application locally, outside a container for now.

Migrations create:

- `users`: UUID, unique normalized email, password hash, and creation timestamp.
- `sessions`: UUID, user, expiry, and revocation timestamp.
- `refresh_tokens`: unique token hash, session, creation and use timestamps.
- Foreign keys and indexes for email, token lookup by hash, and tokens by session.

Define GORM models with explicit UUID and timestamp fields, without embedding `gorm.Model` or implicit soft deletion.

## Registration and HTTP API

Register with email and password, without email verification, issuing tokens immediately. Every login creates an independent device session.

Place all routes under `/api/v1/auth`:

| Method and route | Behavior |
|---|---|
| `POST /register` | Accepts `email`, `password`; creates a user and session; returns `201` |
| `POST /login` | Accepts `email`, `password`; creates a session; returns `200` |
| `POST /refresh` | Reads the refresh cookie, rotates the refresh token, and returns a new JWT |
| `POST /logout` | Revokes the current refresh session and clears the cookie; idempotent `204` |
| `GET /me` | Verifies the Bearer JWT and returns `id`, `email`, `created_at` |

Registration and login return `user`, `access_token`, `token_type: "Bearer"`, and `expires_in`. Refresh returns token fields without `user`. The refresh token is transmitted only through a cookie.

Errors use `{"error":{"code":"…","message":"…"}}`: `400` for invalid input, `401` for invalid credentials or tokens, `409` for an existing email, and `500` for internal failures. Login must not reveal whether a user exists; errors must not expose database details.

## Passwords and token lifecycle

- Trim email, lowercase it, and validate its format. Do not transform passwords; require 12–128 characters.
- Hash passwords with Argon2id and an individual random salt: 64 MiB memory, 3 iterations, parallelism 1. Store parameters alongside the hash.
- Access token: **JWT**, HS256, 15-minute expiry. Verify the signature, algorithm, and required `sub`, `iss`, `aud`, `iat`, `exp` claims.
- Refresh token: 32 random bytes encoded as base64url; store only its SHA-256 hash.
- Sessions last 30 days from login without extension. Issue a new token pair on every refresh.
- Create the user and first session in one GORM transaction.
- Rotate in a transaction, locking the session row with `clause.Locking{Strength: "UPDATE"}`; recheck token state after acquiring the lock.
- Keep used hashes until session expiry. Reuse revokes the entire corresponding session without affecting other devices.
- Reuse-triggered revocation must commit **before** returning `401`; authorization errors must not roll back revocation.
- Concurrent refresh with the same token counts as reuse; clients must refresh sequentially.
- Logout revokes the refresh session; previously issued JWTs remain valid until expiry.

Cookie `refresh_token`: `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/api/v1/auth`, no `Domain`. Allow disabling `Secure` through configuration for local HTTP.

Check mutating request `Origin` against the allowlist; require `X-CSRF-Protection: 1` when `Origin` is absent. CORS permits only the configured origin and credentials.

Environment configuration: HTTP address, database URL, JWT secret, issuer, audience, origin, and cookie mode. The JWT secret is required and must contain at least 32 random bytes. Never log passwords, tokens, cookies, or sensitive SQL parameters. Token responses use `Cache-Control: no-store`.

## Validation and acceptance criteria

- Verify Docker Compose, PostgreSQL readiness, migrations on an empty database, and persistence after container recreation.
- Unit tests: validation, passwords, JWT issuance, invalid signatures, algorithms, issuer, audience, and expiry.
- Run GORM integration tests against real PostgreSQL through a separate `TEST_DATABASE_URL`, not SQLite.
- Verify registration, concurrent registration with the same email, login, `/me`, cookies, CORS, and mutating request protection.
- Verify rotation, reuse, committed revocation after `401`, concurrent refresh, expiry, logout, and device independence.
- Run `gofmt`, `go vet ./...`, `go test ./...`, and the build.
- Document database, migration, and API startup in README, with a registration → `/me` → refresh → logout `curl` workflow.

Password recovery, email verification, and frontend implementation are out of scope. The frontend and API are assumed to share a site; locally, `localhost:3000` and `localhost:8080`.

## Tasks

Mark a task complete (`[x]`) only after implementation and the corresponding validation. Leave partial or blocked tasks unchecked (`[ ]`) and explain why next to the item. Update this list during implementation; record validation results and limitations alongside the relevant task.

### 1. Environment and PostgreSQL

- [x] Check Go 1.26+, Docker, and Docker Compose; start Docker Desktop and wait for engine readiness.
- [x] Add `backend/docker-compose.yml` with PostgreSQL 17, a named volume, healthcheck, and port binding to `127.0.0.1`.
- [x] Add `.env.example` with database/application settings; prepare an ignored local `.env` with local secrets.
- [x] Validate Compose configuration, run `docker compose up -d --wait`, and confirm PostgreSQL availability.

### 2. Application foundation and configuration

- [x] Add GORM, the PostgreSQL driver, goose, JWT, and Argon2id; pin dependencies in `go.mod` and `go.sum`.
- [x] Create HTTP, authentication service, token, and repository packages under `internal/`.
- [x] Implement environment configuration loading and validation, including the required JWT secret and cookie/CORS settings.
- [x] Configure GORM connectivity, a single `database/sql` pool, connection checks, and resource cleanup.
- [x] Wire the HTTP server in `cmd/api`, graceful shutdown, and logging without secrets or sensitive SQL parameters.

### 3. Schema, migrations, and repositories

- [x] Create SQL migrations for `users`, `sessions`, and `refresh_tokens` with unique constraints, foreign keys, and indexes.
- [x] Add a separate goose command for applying, inspecting, and rolling back migrations; exclude `AutoMigrate` from API startup.
- [x] Define GORM models with UUID and timestamp fields, without `gorm.Model` or soft deletion.
- [x] Implement user, session, and refresh token repositories with context propagation and shared transaction support.
- [x] Implement session lookup/locking for rotation, used hash retention, and session revocation.
- [x] Apply migrations to an empty database; verify reruns and rollback/reapplication on a separate test database.

### 4. Passwords and tokens

- [x] Implement email normalization/validation and password length checks without modifying passwords.
- [x] Implement Argon2id hashing and verification with salt and stored parameters.
- [x] Implement 15-minute JWT issuance and signature, HS256, and required claim verification.
- [x] Implement refresh tokens from 32 random bytes, base64url encoding, and SHA-256 hashing.
- [x] Implement fixed 30-day session expiry and expiry/revocation checks.

### 5. Authentication flows

- [x] Implement registration: transactional creation of user, session, and refresh token; handle email conflicts.
- [x] Implement login with identical errors for unknown email and incorrect password; create an independent session for each login.
- [x] Implement atomic refresh rotation with session locking and token state revalidation after locking.
- [x] Detect token reuse and commit session revocation before returning an authorization error.
- [x] Implement idempotent logout for the current session without revoking other devices.
- [x] Retrieve the current user by the ID from a verified JWT.

### 6. HTTP API and request protection

- [x] Add request/response DTOs and a consistent error format with `400`, `401`, `409`, `500`, without internal details.
- [x] Wire `POST /api/v1/auth/register` and `POST /api/v1/auth/login`, returning JWT in JSON and refresh only in a cookie.
- [x] Wire `POST /api/v1/auth/refresh` and `POST /api/v1/auth/logout`, replacing and clearing cookies respectively.
- [x] Add Bearer JWT middleware and protected `GET /api/v1/auth/me`.
- [x] Configure refresh cookie attributes and local disabling of `Secure`; limit cookie lifetime to the remaining session duration.
- [x] Implement CORS, preflight, `Origin` checks, and `X-CSRF-Protection: 1` for mutating requests without `Origin`.
- [x] Add `Cache-Control: no-store` to token responses; verify tokens and passwords do not appear in logs or errors.

### 7. Automated tests

- [x] Add unit tests for email, password length boundaries, and Argon2id hashing/verification.
- [x] Add JWT unit tests: valid tokens, invalid signatures, algorithms, issuer, audience, missing claims, and expiry.
- [x] Prepare integration tests with dedicated PostgreSQL via `TEST_DATABASE_URL`, migrations, and isolated test data.
- [x] Verify registration, duplicate/concurrent registration of the same email, and successful/failed login.
- [x] Verify `/me`, response/error formats, cookies, CORS, preflight, and mutating request protection.
- [x] Verify successful rotation, unknown tokens, expired/revoked sessions, reuse, and revocation persistence after `401`.
- [x] Verify concurrent refresh: one token must not create two valid session branches.
- [x] Verify repeated logout, device independence, and validity of previously issued JWTs until expiry after logout.
- [x] Verify rollback of incomplete registration and rotation on database write failures.

### 8. Documentation and final validation

- [x] Update README: environment, configuration, Docker/database startup, migrations, API startup, and test commands.
- [x] Document Compose shutdown with data retention and, separately, local database deletion.
- [x] Add `curl` examples with a cookie jar and CSRF header for registration, login, `/me`, refresh, and logout.
- [x] Document token lifetimes, sequential refresh, reuse behavior, and logout limitations.
- [x] Run formatting, `go vet ./...`, `go test ./...`, PostgreSQL integration tests, and the backend build.
- [x] Verify user/session persistence after API restart and container recreation without deleting the volume.
- [x] Run the full README registration → `/me` → refresh → logout workflow against PostgreSQL in Docker.
- [x] Compare the result against the plan and update every checkbox based on completed work and validation.

### Implementation results

- Go 1.27.1 was downloaded and verified with SHA-256 in `/private/tmp/uptime-toolchain`; the system Go installation was unchanged.
- Docker is running; PostgreSQL 17 is healthy. Another service occupies port `5432`, so local `.env` uses `POSTGRES_PORT=5433`. Compose retains a configurable port defaulting to `5432`.
- The migration was applied locally; unit tests, integration tests on `uptime_test`, `go vet`, and the build passed. Concurrent registration/rotation, up/down/up, and rollback on write failures were verified.
- Final validation: `gofmt` reported no issues; `go vet ./...`, the build, and `go test -race -count=1 ./...` with PostgreSQL passed.
- The `curl` registration → `/me` → refresh → logout → login flow passed. After stopping the API and running `docker compose up -d --force-recreate --wait postgres`, the saved user and refresh session remained available. The test session was ended with logout.
- API running on `127.0.0.1:8080`, PostgreSQL healthy on `127.0.0.1:5433`. All 50 tasks completed; password recovery, email verification, and frontend implementation were not added.

### 9. Controllers within modules

- [x] Move the authentication HTTP controller and integration tests to `internal/auth/`; remove `internal/httpapi/`.
- [x] Extract shared HTTP middleware and JSON responses into `internal/httpx/`; compose routes through `RegisterRoutes` in `cmd/api/`.
- [x] Verify the build, `go vet`, and all PostgreSQL tests after the move without changing the HTTP contract. Validated: `go test -race -count=1 ./...`, `go vet ./...`, `go build -o bin/api ./cmd/api`.

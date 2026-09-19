# Backend Guidelines

## Project Structure

This directory contains the Go module `uptime-app/backend`. `cmd/api/main.go` starts the authentication HTTP API. Packages under `internal/` handle configuration, GORM persistence, authentication, and HTTP. Versioned SQL migrations live under `migrations/`; run them with `cmd/migrate`.

## Development Commands

Requires Go 1.26+. Run commands from `backend/`:

- `go run ./cmd/api`: start the API after exporting `.env` and applying migrations.
- `go build -o bin/api ./cmd/api`: compile the executable.
- `go test ./...`: run unit tests; PostgreSQL tests require `TEST_DATABASE_URL`.
- `gofmt -w .`: format Go source files.

## Coding Style & Naming

Use `gofmt` formatting, including tabs for indentation. Use lowercase package names and PascalCase for exported names. Keep executable setup in `cmd/api/` and reusable internal code under `internal/`.

## Testing

Use Go's standard `testing` package with colocated `*_test.go` files and `TestXxx` functions. No coverage threshold is configured. Run `go test ./...`, `go vet ./...`, and the build command above. Integration tests require a dedicated PostgreSQL database ending in `_test` via `TEST_DATABASE_URL`; each test isolates data in its own schema. Follow README setup commands. Report skipped integration tests or an unavailable toolchain.

## Dependencies & Configuration

Keep dependencies in `go.mod` and, when generated, `go.sum`. Run `go mod tidy` after dependency changes. Build outputs belong in the ignored `bin/` directory. Keep local secrets in ignored environment files; any committed `.env.example` must contain placeholders only.

Use GORM only in persistence code. Apply explicit goose migrations; do not introduce `AutoMigrate`. Preserve atomic refresh rotation and commit session revocation before returning an authorization error. Never log credential values or SQL parameters.

Keep HTTP controllers and their route registration inside their owning feature module (for example, `internal/auth/controller.go`). Shared HTTP middleware and response helpers belong in `internal/httpx`; compose module routes in `cmd/api`. Do not reintroduce a centralized controller package.

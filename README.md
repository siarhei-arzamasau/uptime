# Uptime App

Two independent projects: a Go API and a Next.js interface for registration, login, profiles, and website monitors. Users can save website URLs with configurable check intervals; actual monitoring checks are not implemented yet.

```text
backend/
  cmd/api/main.go    # Authentication HTTP API
  internal/          # Authentication, HTTP, and database access
  go.mod
frontend/
  src/app/           # Base Next.js App Router
  package.json
  package-lock.json
```

## Backend

Requires Go 1.26+.

PostgreSQL setup with Docker Compose, migrations, API startup, and testing instructions: [backend/README.md](backend/README.md).

Implementation plan and task status: [docs/authentication-plan.md](docs/authentication-plan.md).

## Frontend

Next.js 16, TypeScript, ESLint. Requires Node.js 22.12+ and npm.

```sh
cd frontend
npm ci
npm run dev
```

The frontend is available at http://localhost:3000.

Validation and build: run `npm run lint` and `npm run build` from `frontend/`.

## Combined startup

Requires Node.js 22.12+, Go 1.26+, Docker Compose, installed npm dependencies, and local `backend/.env` and `frontend/.env.local` files (see the examples).

```sh
node scripts/dev.mjs
```

The script starts PostgreSQL, applies migrations, and launches the API and frontend. Ctrl+C stops both applications while preserving the database. If Go is not on PATH, set `GO_BIN=/absolute/path/to/go` in `backend/.env`. On macOS, install Go permanently with `brew install go`.

Configuration details, themes, and tests: [frontend/README.md](frontend/README.md). Plan and checklist: [docs/frontend-authentication-plan.md](docs/frontend-authentication-plan.md).

# Product architecture for review

Read this before the checklist. These paths describe the current product, not a mandate to add new layers. Current `AGENTS.md` files override this snapshot when the architecture changes.

## Request flow and responsibilities

| Boundary | Current implementation | Review invariant |
| --- | --- | --- |
| Browser UI | `frontend/src/app/` pages/layouts, `src/features/auth/`, `profile/`, `monitors/`, `src/components/` | Render user-safe data and feature state. Pages compose features; components do not query PostgreSQL or obtain raw auth tokens. |
| Browser transport | `features/auth/client.ts`, `transport.ts`; feature clients reuse this transport | Call same-origin BFF routes, serialize cookie-changing operations with Web Locks across tabs, deduplicate session checks, keep tokens out of JavaScript. |
| Next.js HTTP/BFF | Thin `src/app/api/**/route.ts`; `features/auth/server.ts` with `import "server-only"`; `features/monitors/server.ts` uses `handleAuthenticated` | The auth server adapter owns upstream Go access and credential recovery; feature adapters supply validation, decoding, and operation descriptions. Routes delegate rather than duplicating auth. Browser error/status/envelope mappings can differ from the Go API. |
| Go composition | `backend/cmd/api/main.go` | Wire configuration, store, token/service/controller dependencies, module routes and `httpx.Protect`; startup is not a controller or migration engine. |
| Go HTTP modules | `internal/auth/controller.go`, `profile/controller.go`, `profile/avatar.go`, `monitor/controller.go` | Owning modules register routes and decode/bound inputs, authenticate/derive ownership, and map results to DTOs/errors. Existing profile/monitor controllers call store methods directly; do not demand extra services/interfaces merely for symmetry. |
| Go auth policy | `internal/auth/service.go`, `crypto.go`, `password_gate.go` | Own credential normalization, hashing, JWT verification, session lifecycle, rotation/revocation, and bounded password work. Keep multi-write invariants atomic. |
| Shared HTTP | `internal/httpx/` | Shared CORS/CSRF, request context deadlines, no-store headers and JSON error envelopes. No centralized feature-controller package. |
| Persistence | `internal/store/` (GORM/PostgreSQL models and repositories) | GORM stays here. Ownership-aware queries, explicit row locks/transactions, context-bound database operations. Credentials/storage-only fields stay out of response DTOs. |
| Schema lifecycle | `backend/migrations/`, `cmd/migrate`, `docs/guides/migrations.md` | Explicit embedded goose migrations; never `AutoMigrate`. Applying migrations is a separate, verified database operation. |
| API documentation | Controller swaggo annotations; `backend/cmd/api/main.go`; `backend/docs/swagger.json`, `swagger.yaml`, `contract_test.go` | Real request/response types define the backend contract. Regenerate and commit JSON/YAML with API changes. `make redoc` produces an ignored local HTML artifact; BFF routes are not described by this Go contract. |
| Shared validation/startup | `contracts/monitor-urls.json`, `scripts/dev.mjs`, `dev.test.mjs` | Shared URL examples exercise Go and TS policy. Launcher keeps environment/dependency ownership separate; E2E uses a dedicated database and avatar directory. |

## Security and lifecycle details that are easy to break

- Go issues 15-minute access JWTs and HttpOnly `refresh_token` cookies scoped to `/api/v1/auth`. Next's BFF stores both tokens in HttpOnly `uptime_access` and `uptime_refresh` cookies; no tokens in React props, JSON responses, localStorage or readable cookies. Only theme preference is intentionally browser-readable.
- Refresh sessions have a fixed 30-day lifetime from login. Rotation consumes a token under a session lock, reuses the original expiry, and revokes replayed sessions **before** returning unauthorized. Other sessions remain independent. Logout is idempotent; previously issued JWTs remain valid until expiry.
- SSR/prefetch must not rotate refresh cookies. Client session recovery uses Web Locks because tabs share cookies. After successful rotation, a later non-401 `APIError` must retain replacement cookies; transient failures should preserve mounted drafts and committed updates should supersede stale revalidation.
- Go rejects foreign Origins and requires `X-CSRF-Protection: 1` for mutations without Origin. The BFF requires matching Origin and its CSRF header. Authentication, CSRF and ownership are separate checks.
- Password work has bounded per-process admission and concurrency; context cancellation does not interrupt an admitted Argon2 hash. `httpx`'s context deadline is cooperative, not a forced HTTP timeout. Do not document stronger cancellation guarantees.
- Profile names are trimmed, limited by Unicode characters, and reject controls; an empty name clears it. Avatar uploads require exactly one name and one JPEG/PNG file, at most 5 MiB and 2048 × 2048 pixels, decoded and re-encoded without metadata. Generated UUID filenames are the only public file paths; filesystem and database failure cleanup must agree.
- Monitor ownership comes from verified JWT identity. Listing uses an exclusive `(created_at, id)` cursor and at most 50 entries. Creation validates URL policy and a whole-number interval in PostgreSQL INTEGER range. Saved configuration is implemented; outbound checks, jobs and uptime results are not.

## Test and dependency ownership

Backend uses Go's standard tests, PostgreSQL integration tests with `_test` database names and per-test schemas, and contract freshness/coverage tests. Frontend uses Vitest/RTL plus Playwright desktop/mobile through the dedicated E2E launcher. Neither project has a numeric coverage threshold. Keep `backend/go.mod`/`go.sum` and `frontend/package.json`/`package-lock.json` within their own projects. Reuse real failure-boundary tests; do not require tests for reversible documentation-only edits or impose a new framework.

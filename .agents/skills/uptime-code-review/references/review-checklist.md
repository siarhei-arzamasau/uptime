# Review checklist

Read on every review. Apply **all common rows**, then select the conditional sections from changed files and affected behavior. Record a status and evidence for each applicable row; good/bad indicators are investigation cues, not automatic findings. Use the architecture reference and current project instructions together.

## Common: every diff

| Check | Good indicators | Bad indicators to investigate |
| --- | --- | --- |
| Scope and executable evidence | Correct merge base/commit, complete diff, relevant callers and tests; static results read | Checks on another checkout, omitted local files/deletions, an inferred PR base, failures called a pass |
| Behavior and error boundaries | Supported success, failure, retry and cancellation flows remain coherent | Dropped errors, false-success responses, a retry repeating a consumed or non-idempotent operation |
| Secrets and privacy | Placeholders in examples, ignored local config, safe DTOs/logging, redacted review evidence | Live keys/credentials/private keys in added code or fixtures, DSN/password/token output, credential-bearing URLs, tracked `.env` or build/dependency artifacts |
| Security boundaries | Authentication plus resource ownership, trusted input boundaries and safe encoding | Trusting client IDs, trusting MIME alone, path traversal, XSS via HTML insertion, permissive CORS, missing CSRF on mutations |
| Naming and contracts | Names communicate units/domain/ownership; exported functions document returns/errors/context where applicable | Seconds/minutes confused, booleans whose meaning is hidden, misleading names or comments concealing changed behavior |
| Dead code and dependencies | Referenced paths/exports, reachable branches, focused dependencies with synchronized locks | Unreachable branches, orphaned handlers, unused exports or speculative wrappers with an identified maintenance cost; check callers and framework entrypoints before calling code dead |
| Architecture | Responsibilities remain within the concrete boundaries in `architecture.md` | GORM outside store, raw upstream auth fetch duplicated in UI/feature routes, centralized Go feature controllers, abstractions added without a current need |
| Test adequacy | Regression exercises the changed failure/invariant and meaningful assertions; existing tests reused | Tests only mirror implementation, happy path for a changed security/transaction boundary, missing concurrency/retry coverage, all integrations skipped without disclosure |
| Performance/resource bounds | Bounded bodies, work admission, queries, pagination and listeners; measure affected critical paths | Unbounded image/password allocations, N+1 queries, growing request queues, redundant fetches, subscriptions/timers without cleanup |
| API/config/docs | Callers, contract, migrations and setup commands agree; version-specific behavior verified | Contract drift, mismatched envelopes/status codes, unsynchronized dependency locks, command docs that no longer run |

## Go backend: backend code, schema, configuration, or affected consumers

| Check | Good indicators | Bad indicators to investigate |
| --- | --- | --- |
| Controller responsibilities | Feature-owned registration; bounded decoding; unknown JSON fields rejected; correct status/DTO | Shared controller dumping ground; database models exposing password/session fields; request ownership used without verification |
| Persistence and SQL | Context propagated through store; parameterized queries; user-scoped reads/writes | Concatenated SQL/unsafe raw query inputs, missing ownership predicate, ignored database errors; do not flag fixed constant SQL merely because it is raw |
| Transactions and locks | Related user/session/token writes commit together; session/user locks cover the entire check/write sequence | Check outside lock, partial token/session state, a revoke rolled back when unauthorized is returned |
| Auth/crypto, when affected | Allowed JWT algorithm/issuer/audience/expiry checked, random tokens stored as hashes, fixed supported Argon2 costs and bounded admission | Algorithm confusion, raw refresh tokens persisted, easier stored hash costs accepted, unbounded concurrent password work |
| Refresh/logout, when affected | Single-use rotation, fixed expiry, replay revokes only the relevant session; idempotent logout | Extending TTL on rotation, simultaneous refresh accepted without serialization, failed revocation reported as success, claiming JWT logout invalidation |
| HTTP/context | Cooperative deadlines documented; CORS/CSRF kept; request/body and header limits; internal errors sanitized | `context.Background()` losing request cancellation, assuming context abort stops hashing, unrestricted Origins, leaked SQL parameters |
| Public contract | Annotations on each method/path; actual payloads, auth, parameters and errors; regenerated JSON/YAML; `go test ./docs` | Missing route, stale schemas, Bearer/cookie schemes confused, an optional cursor or no-body response marked required/nonempty |
| Profile/upload, when affected | Trimmed Unicode name policy, limits before pixel allocation, re-encoding, generated safe filenames, atomic profile update and cleanup | Raw uploaded bytes served, oversized allocation before validation, unrestricted file path, new file leaked on failed DB update, previous avatar deleted too early |
| Monitors, when affected | Owner from JWT, URL parity with shared cases, bounded integer seconds, stable exclusive cursor | Owner selected by client, ambiguous numeric hosts accepted, unit overflow, cursor bypassing owner filter, unstable order at equal timestamps |

## Next.js frontend/BFF: frontend code, config, UI, or affected contracts

| Check | Good indicators | Bad indicators to investigate |
| --- | --- | --- |
| App Router/version | Thin route handlers, correct async params/cookies and runtime for installed Next; framework rules preserved | APIs recalled from older Next versions, client imports of server code, unnecessary broad client boundary; inspect installed docs |
| BFF/Go boundary | `server-only` auth adapter owns credentials/upstream calls; feature adapter reuses `handleAuthenticated` | Direct Go calls from browser, duplicate credential recovery in a feature adapter, secrets in `NEXT_PUBLIC_*`, props or JSON |
| Browser session transport | Same-origin BFF requests with CSRF, Web Locks shared across tabs, deduplicated session checks | Token-readable storage, locks replaced with only an in-tab promise, refresh from SSR/prefetch or independent racing fetches |
| Cookies/error handling | HttpOnly/Secure/SameSite/path policy maintained; fixed refresh expiry; rotated cookies preserved on later non-401 API errors | Replacement cookie dropped after the old token was consumed, response clearing cookies for a transient 503, incorrect Retry-After/status forwarding |
| Types/untrusted responses | Strict TS, validated unknown payloads, explicit DTOs and errors, no blanket suppression | `any` or unchecked assertions at an upstream boundary; broad `@ts-ignore`/ESLint disables hiding defects; passing `tsc` does not prove runtime shape |
| React state/effects | Stable keys and effect cleanup; derive ordinary state; explicit drafts preserved on transient failures; stale requests cannot overwrite saved edits | Mirrored props drifting, premature unmount of drafts, racing responses, mutation during render, leaked listeners or intervals |
| User-facing errors/forms | Validation parity with Go, recoverable retry, loading/disabled state consistent, auth redirect only on confirmed session loss | UI success after backend failure, duplicate submits, finite/integer checks missing, lost drafts or wrong redirect on network failure |
| UI/accessibility | Labels, keyboard/focus behavior, alerts/status, desktop/mobile screenshots for visible changes | Unlabelled inputs, inaccessible menu/modal, keyboard traps, overflow, missing focus after navigation; focus on changed behavior |
| Fetch/caching/performance | Auth fetches no-store with bounded timeout; redirect policy safe; avoid unnecessary requests/client work | Credential data cached or shared, unlimited upstream wait, uncontrolled request waterfalls; public immutable avatars are an intentional cache exception |

## Conditional cross-cutting workflows

| Trigger | Check and good evidence | Bad indicators to investigate |
| --- | --- | --- |
| SQL migrations | Follow `docs/guides/migrations.md`; immutable applied history, fresh/upgrade data assertions, latest down/up on disposable DB, target host/port/database/schema verified | `AutoMigrate`, editing an already applied migration, rollback on working data, status alone treated as proof, one psql URL pointing elsewhere |
| Auth/integration behavior | Correct `_test` DB and schema isolation; concurrency/rollback/replay tests, frontend desktop/mobile E2E as required by scoped guides | Working DATABASE_URL reused, launcher migrating normal data, skipped tests called passing, test schema accidentally shared |
| Root launcher/shared contracts | Both Go/Next consumers verified; environments remain separate; startup cleanup owns only its resources; validation/encoding agree | Backend secrets entering frontend env, root dependencies mixing both projects, inconsistent URL cases, process/DB cleanup deleting unrelated resources |

## Priority calibration and output

Assign P0 only to a confirmed critical reachable exposure/outage/data-loss scenario, P1 to a demonstrated important failure, P2 to a bounded defect, P3 to a minor actionable cost. Security bugs are not automatically P0; severity depends on reachability and impact. A static warning or a stylistic preference is not a finding on its own. No numeric coverage target or mandatory new architecture layer exists.

For each final candidate, verify its changed location, trigger, effect, remedy and any supporting scoped rule. Merge duplicates. Report findings from highest to lowest priority using normal Markdown and inline directives when appropriate. Include the validation limits without dumping this entire checklist. If nothing actionable remains, say so; do not manufacture a P3 item to fill a category.

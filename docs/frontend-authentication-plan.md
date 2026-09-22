# Frontend authentication, testing, and combined startup

## Interface

English `/login`, `/register`, and protected `/dashboard` pages with email and Sign out. The root redirects to the dashboard; guests are redirected to login. Registration validates password confirmation only on the client. Forms include validation, password visibility, error/loading states, and duplicate submission protection. Light and dark themes use CSS Modules and variables; the system theme is the default, with manual selection stored in a separate cookie and applied without a theme flash. Responsive layout, keyboard access, autocomplete, accessible labels, and errors are required. Monitoring and password recovery are out of scope.

## Session

Next.js Route Handlers `POST /api/auth/{register,login,session,logout}` call Go. JWT and refresh tokens remain in HttpOnly cookies: `uptime_access` (Path=/, 15 minutes) and `uptime_refresh` (Path=/api/auth, original Go expiry). SameSite=Lax, no Domain, Secure in production. Tokens never reach the browser through JSON, props, or Web Storage. Go receives Bearer and refresh_token. BACKEND_URL and APP_ORIGIN are server configuration. Origin and X-CSRF-Protection are checked; caching is disabled.

Session calls /me; missing access or a 401 triggers one refresh and one retry. Invalid refresh clears cookies; network/5xx failures preserve the session for retry. Personal data stays hidden until verification. Verify on load and return to the tab; never refresh during SSR/prefetch. Web Locks serialize auth requests across tabs; concurrent checks within a tab are deduplicated. BroadcastChannel carries only events. Without Web Locks, display a browser update message. Logout requires confirmation from Go.

## Combined startup

`node scripts/dev.mjs`: check tools, Docker, dependencies, environment, and ports; start Compose; apply migrations; build and start Go; wait for a 401 from /me; start Next.js and verify readiness. Do not overwrite local files; process environment takes priority. Never pass backend secrets to Next.js. Include log prefixes, timeouts, and shutdown of both process trees on signals/failures. Keep PostgreSQL and its volume. Do not stop unrelated processes. Restart Go manually after changes; Next.js supports HMR. Ports: 3000/8080; database port comes from configuration.

## Tests

Vitest: Node for server logic, jsdom + Testing Library/user-event/jest-dom for components, V8 coverage without a numeric threshold. Cover cookies, JWT/refresh, CSRF, errors, deduplication, notifications, forms, themes, and the dashboard. Test combined startup with mocked processes.

Playwright Chromium desktop/mobile: real Go and a dedicated PostgreSQL database, `uptime_e2e_test`, ports 3001/8081, unique accounts, migration preparation, and cleanup of owned processes. Cover login/logout, errors, reload, refresh after deleting access, two tabs, service outages, HttpOnly, token leak prevention, themes, and keyboard access. Retain traces/screenshots on failure; do not commit artifacts.

Commands: npm test, test:watch, test:coverage, test:e2e, test:e2e:ui. Final checks: lint, build, Vitest, Playwright, and actual combined startup/shutdown.

## Tasks

Mark [x] only after implementation and validation; document blockers explicitly.

- [x] Configure Vitest, Testing Library, and coverage.
- [x] Configure Playwright and an isolated E2E environment.
- [x] Implement the server-side Go client, cookies, and Route Handlers with tests.
- [x] Implement session management and tab coordination with tests.
- [x] Add forms, dashboard, navigation, and logout with component tests.
- [x] Implement both themes and accessibility checks.
- [x] Add the shared startup script, shutdown handling, and tests.
- [x] Implement E2E scenarios.
- [x] Update README files, environment examples, and frontend instructions.
- [x] Run ESLint, the production build, Vitest, Playwright, and combined startup checks.

## Validation results

- Vitest: 40 tests passed.
- Playwright: 12 desktop/mobile scenarios passed against the real Go API and PostgreSQL `uptime_e2e_test`.
- The shared script started both services for E2E and released ports 3001/8081 after completion. PostgreSQL was preserved.
- The shared launcher and current Vitest require Node.js 22.12+; locally validated with Node.js 24.19.0.
- ESLint and the production build passed. V8 coverage: 94.32% statements, 87.37% branches, 98.14% lines for frontend auth/theme.
- Light/dark theme and mobile registration screenshots were reviewed; no horizontal overflow was found.
- After intentionally terminating the test Go process, the launcher stopped Next.js, exited with code 1, and released both ports.

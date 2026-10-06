# Monitoring validation

Measured on 2026-10-06 against local PostgreSQL 17 in a dedicated Docker container, with Go 1.27.1 (module baseline 1.26) and Node 24.19.0. No working/production database was migrated.

## Functional and migration checks

- Full backend suite and race detector passed with PostgreSQL integration tests enabled.
- Fresh-schema migration, upgrade from version 4 with a stored 1-second interval, idempotent `up`, and `down/up` passed. Upgrade preserves monitors, changes 1–4 seconds to 5, and schedules existing rows. Rollback removes monitoring results and does not restore the old interval.
- Tests cover competing workers, lease expiry, duplicate completion, stale configuration results, deletion, ownership, retention, weighted/null buckets, deadlines, public-IP policy, redirects, and cancellation without fabricated downtime.
- 167 frontend component/BFF/launcher tests, lint, production build, and 26 desktop/mobile E2E scenarios passed. The monitoring E2E uses a real worker against an `.invalid` host for a deterministic DNS failure and persisted history; recovery/stale UI transitions use explicitly mocked status responses. Real HTTP success and non-200 behavior are tested separately in the worker suite.
- E2E screenshots capture history in both themes at the configured desktop/mobile sizes. They do not replace the required manual Playwright MCP check.

## Synthetic load and storage

Run with `TEST_DATABASE_URL` pointing to a dedicated `_test` database:

```sh
MONITOR_LOAD_TEST=1 go test -run '^TestMonitoringLoad$' -v ./internal/monitor
```

The test creates/removes its own schema, uses the real worker and PostgreSQL, and injects a controlled HTTP transport; it does not measure external DNS/TLS/network costs. Each scenario runs for 22 seconds with 1000 points, 5-second intervals, and 128 concurrent slots. These measurements are a local feasibility check, not a production SLA.

| Scenario | Saved observations | Rate | SQL operations/s¹ | p95 / max schedule lag | Peak active requests |
| --- | ---: | ---: | ---: | ---: | ---: |
| 10 ms success | 4512 | 205.1/s | 1027.5 | 3507 / 3508 ms | 128 |
| 10 s timeout | 256 | 11.6/s | 64.1 | 21009 / 21009 ms | 128 |

¹ GORM statement callbacks, excluding transaction-control commands. The startup batch makes all 1000 points due together. Timeout overload stays bounded; overdue observations become stale rather than fabricating extra failures. Raising concurrency or adding workers is necessary to sustain 200/s with slow endpoints.

The fast run allocated approximately 231 MB total over 22 seconds; the ending Go heap was about 3.65 MB (not peak RSS). The timeout run ended at about 4.61 MB heap, with about 15.6 MB total allocations. There is no unbounded in-memory queue.

For 10 monitors at 43,200 minute rows each:

- 432,000 rows: table 29,515,776 bytes, indexes 38,985,728 bytes.
- Linear estimate for 1000 monitors: **6,850,150,400 bytes (~6.85 GB)**, excluding WAL, transient updates, table/index bloat, monitor state, and backups. The full 43.2-million-row production case was not materialized.
- Reading one monitor's 30-day history: about 11.7 ms.
- Deleting all 432,000 expired rows in 5000-row batches: about 613 ms of direct database calls. The production cleanup loop additionally yields 50 ms between batches.

## Manual browser verification — passed

Completed on 2026-10-06 through Playwright MCP 0.0.83 and its Chromium browser, against this checkout started with `node scripts/dev.mjs --e2e`. Frontend: `http://localhost:3001`; API: `127.0.0.1:8081`; PostgreSQL: local dedicated container on port 55436, database `uptime_e2e_test`, schema `public`, already at migration 5. A new synthetic account was used. All three monitors created for this check were deleted through the UI, deletion survived reload, and the account was signed out. No production data was used.

| Criterion | Result and evidence |
| --- | --- |
| Creation and validation | Passed: 4-second interval rejected; 5-second interval accepted; real worker GET to `https://example.com` produced Working / HTTP 200. |
| Failure and recovery on the same URL | Passed without response mocks: `https://httpbin.org/status/200,503` produced HTTP 200 at 07:27:16, HTTP 503 at 07:27:36, and HTTP 200 at 07:27:56 (Europe/Minsk). UI switched Working → Unavailable → Working. History contained both successes and failures, including 12/18 = 66.67%. |
| URL and interval edits | Passed: interval changed from 5 to 10 seconds with existing samples retained; URL-change warning appeared before saving; switching to an example.com missing path returned HTTP 404 and reset history; switching back returned HTTP 200 with fresh history. Settings and results survived reload. |
| Pending, missing and stale data | Passed: while the test worker was paused, a new monitor showed Awaiting first check and No data, with zero samples. Existing points became No fresh data while retaining their last HTTP result. Resuming the worker restored fresh status; the `.invalid` monitor reported DNS lookup failed. |
| History | Passed: default 24 hours, all four periods, corresponding steps 60/300/1800/7200 seconds, one expanded chart, explicit empty buckets, mixed-result percentages, and automatic history/status refresh. |
| Keyboard and touch | Passed: Home/End selected empty/latest buckets with accessible detail; Chromium touch events moved the slider on mobile. Delete cancellation initially focused Cancel and restored focus to the originating button. |
| Visibility handling | Passed with a controlled browser visibility event: overriding both `document.hidden` and `visibilityState` stopped requests for 7 seconds; restoring visibility immediately requested status and history. Native tab switching in this automation session kept the page visible, so this check is explicitly simulated. No monitoring responses were mocked. |
| Layout and themes | Passed: screenshots captured and visually inspected at 1440×900 and 390×844 in light/dark themes; no horizontal overflow, clipped controls or application-layer overlap. Edit warning and deletion confirmation also inspected on mobile. The Next.js development indicator is visible in screenshots. |
| Deletion and adjacent authentication | Passed: cancellation retained the point; confirmed deletion returned HTTP 200 for all three test monitors; reload showed No websites yet; sign-out returned to login. |
| Console/network | Passed for affected flows: registration returned 201; monitoring CRUD, status and history requests succeeded. No JavaScript errors or monitoring request failures. One unrelated HTTP 404 for the application's absent `/favicon.ico` was recorded; the base branch also has no favicon asset. |

Screenshot evidence (captured during real public HTTP checks; results naturally differ between captures):

| Desktop | Mobile |
| --- | --- |
| [Light](images/monitoring/desktop-light-mixed-history.png) | [Light](images/monitoring/mobile-light-mixed-history.png) |
| [Dark](images/monitoring/desktop-dark-mixed-history.png) | [Dark](images/monitoring/mobile-dark-mixed-history.png) |

This follow-up changes documentation and evidence only. The previously passed automated suites above were not rerun because application code did not change; API and worker were rebuilt by the E2E launcher. No monitoring regressions requiring code changes were found.

## Rollout

Apply version 5 during a coordinated update of the API (old writers do not create schedules and can submit intervals below 5), then start worker and frontend. Existing monitors become due immediately. Watch schedule lag, active/completed counts, stale statuses, and database/cleanup errors. This task validates migration code only; applying it to a persistent deployment requires its normal backup and target checks. Reverting the migration deletes monitoring history.

## Review fixes — 2026-10-06

Commit `c04ed55` fixes both P2 review findings: creation returns the committed initial pending status without a post-commit database read, and failed claim transactions expose no dispatchable jobs. Worker dispatch also rejects populated batches accompanied by errors. Three regression tests reproduced the failures before the fix and passed afterward, including a deferred PostgreSQL trigger that rejects commit after rows have been scanned. Full backend tests with PostgreSQL, race, vet, API/worker builds and OpenAPI freshness passed; generated contracts were updated.

The follow-up Playwright MCP check passed on the fixed commit in the same dedicated E2E environment described above. Creating one monitor returned HTTP 201 with version 1, pending status and null result fields; polling then displayed Working / HTTP 200 from a real GET. Reload retained exactly one monitor. Desktop 1440×900 and mobile 390×844 screenshots were inspected, with no overflow or layout regression. The monitor was deleted through the UI and the account signed out. All monitoring requests succeeded; console inspection found only the existing favicon 404 and the expected session 401 after sign-out, with no JavaScript errors. This completes the browser verification that was unavailable in the separate automated review session. Frontend source and styling were unchanged, so the earlier full frontend and theme validation remains applicable.

Evidence: [desktop](images/monitoring/review-fix-desktop.png), [mobile](images/monitoring/review-fix-mobile.png).

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

## Browser verification limitation

**Manual Playwright MCP verification remains blocked:** this session exposes no Playwright MCP tools. Automated E2E and inspected screenshot artifacts passed, but the repository's manual MCP requirement has not been satisfied. Before marking the change ready for review, run the documented MCP verification on the implementation checkout at 1440×900 and 390×844, exercise creation/edit/deletion/status/history, inspect console/network errors, and attach screenshots.

## Rollout

Apply version 5 during a coordinated update of the API (old writers do not create schedules and can submit intervals below 5), then start worker and frontend. Existing monitors become due immediately. Watch schedule lag, active/completed counts, stale statuses, and database/cleanup errors. This task validates migration code only; applying it to a persistent deployment requires its normal backup and target checks. Reverting the migration deletes monitoring history.

package monitor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"uptime-app/backend/internal/store"
	"uptime-app/backend/migrations"
)

type measuredChecks struct {
	*store.Store
	mu   sync.Mutex
	lags []int64
}

func (s *measuredChecks) ClaimChecks(ctx context.Context, limit int) ([]store.CheckJob, error) {
	jobs, err := s.Store.ClaimChecks(ctx, limit)
	s.mu.Lock()
	for _, job := range jobs {
		s.lags = append(s.lags, max(0, time.Since(job.ScheduledAt).Milliseconds()))
	}
	s.mu.Unlock()
	return jobs, err
}

// TestMonitoringLoad is opt-in because it runs for about one minute and seeds a retention sample.
// It only uses a generated schema in a dedicated _test database, and never sends external HTTP.
func TestMonitoringLoad(t *testing.T) {
	if os.Getenv("MONITOR_LOAD_TEST") != "1" {
		t.Skip("set MONITOR_LOAD_TEST=1 for the 1000-monitor load test")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("a dedicated TEST_DATABASE_URL ending in _test is required")
	}
	admin, err := store.Open(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "load_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.DB.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	defer admin.DB.Exec("DROP SCHEMA " + schema + " CASCADE")
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := store.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db, err := s.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}
	owner := store.User{ID: uuid.New(), Email: "load@example.invalid", Name: "Load test", PasswordHash: "unused", CreatedAt: time.Now()}
	if err := s.DB.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Exec(`INSERT INTO monitors(id, user_id, url, interval_seconds, created_at)
  SELECT gen_random_uuid(), ?, 'https://load.example/health', 5, now() FROM generate_series(1,1000)`, owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Exec("INSERT INTO monitor_states(monitor_id) SELECT id FROM monitors").Error; err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int64
	callback := func(*gorm.DB) { queries.Add(1) }
	for _, register := range []func(string, func(*gorm.DB)) error{
		s.DB.Callback().Query().After("gorm:query").Register,
		s.DB.Callback().Row().After("gorm:row").Register,
		s.DB.Callback().Raw().After("gorm:raw").Register,
		s.DB.Callback().Update().After("gorm:update").Register,
	} {
		if err := register("load:count", callback); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []string{"fast", "timeouts"} {
		t.Run(scenario, func(t *testing.T) {
			if err := s.DB.Exec("UPDATE monitor_states SET next_check_at=now(), job_id=NULL, lease_until=NULL").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Exec("TRUNCATE monitor_minutes").Error; err != nil {
				t.Fatal(err)
			}
			worker, err := NewWorker(s, 128)
			if err != nil {
				t.Fatal(err)
			}
			measured := &measuredChecks{Store: s, lags: make([]int64, 0)}
			worker.store = measured
			var active, peak atomic.Int64
			worker.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				current := active.Add(1)
				defer active.Add(-1)
				for previous := peak.Load(); current > previous; previous = peak.Load() {
					if peak.CompareAndSwap(previous, current) {
						break
					}
				}
				if scenario == "timeouts" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				timer := time.NewTimer(10 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-r.Context().Done():
					return nil, r.Context().Err()
				case <-timer.C:
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), 22*time.Second)
			defer cancel()
			queries.Store(0)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			worker.Run(ctx)
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			var samples int64
			if err := s.DB.Raw("SELECT coalesce(sum(successes+failures),0) FROM monitor_minutes").Scan(&samples).Error; err != nil {
				t.Fatal(err)
			}
			sort.Slice(measured.lags, func(i, j int) bool { return measured.lags[i] < measured.lags[j] })
			if len(measured.lags) == 0 {
				t.Fatal("no checks started")
			}
			p95 := measured.lags[(len(measured.lags)-1)*95/100]
			t.Logf("%s: samples=%d elapsed=%s samples/s=%.1f SQL=%d SQL/s=%.1f lag_p95_ms=%d lag_max_ms=%d peak_requests=%d heap_before=%d heap_after=%d allocated=%d",
				scenario, samples, elapsed.Round(time.Millisecond), float64(samples)/elapsed.Seconds(), queries.Load(), float64(queries.Load())/elapsed.Seconds(), p95, measured.lags[len(measured.lags)-1], peak.Load(), before.HeapAlloc, after.HeapAlloc, after.TotalAlloc-before.TotalAlloc)
			if peak.Load() > 128 || active.Load() != 0 {
				t.Fatal("request concurrency bound or shutdown failed")
			}
			if scenario == "fast" && (samples < 3500 || p95 > 5000) {
				t.Fatal("fast-check throughput or scheduling latency below target")
			}
			if scenario == "timeouts" {
				var stale int64
				if err := s.DB.Raw(`SELECT count(*) FROM monitor_states WHERE last_started_at IS NOT NULL
     AND last_started_at + interval '20 seconds' < now()`).Scan(&stale).Error; err != nil {
					t.Fatal(err)
				}
				if stale == 0 {
					t.Fatal("overload did not expose stale observations")
				}
			}
		})
	}
	// Sample 10 monitors at full 30-day retention, rather than projecting bytes from empty tables.
	if err := s.DB.Exec("TRUNCATE monitor_minutes").Error; err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := s.DB.Exec(`INSERT INTO monitor_minutes(monitor_id, minute_start, successes, failures)
  SELECT m.id, date_trunc('minute', now()) - i * interval '1 minute', 12, 0
  FROM (SELECT id FROM monitors LIMIT 10) m CROSS JOIN generate_series(0,43199) i`).Error; err != nil {
		t.Fatal(err)
	}
	var sizes struct {
		TableBytes int64
		IndexBytes int64
		Rows       int64
	}
	if err := s.DB.Raw(`SELECT pg_table_size('monitor_minutes') AS table_bytes,
  pg_indexes_size('monitor_minutes') AS index_bytes, (SELECT count(*) FROM monitor_minutes) AS rows`).Scan(&sizes).Error; err != nil {
		t.Fatal(err)
	}
	t.Logf("retention sample: rows=%d table_bytes=%d index_bytes=%d seed_elapsed=%s projected_1000_monitors_bytes=%d (linear estimate, excludes bloat/WAL)", sizes.Rows, sizes.TableBytes, sizes.IndexBytes, time.Since(start).Round(time.Millisecond), (sizes.TableBytes+sizes.IndexBytes)*100)
	var row struct{ ID uuid.UUID }
	if err := s.DB.Raw("SELECT id FROM monitors LIMIT 1").Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if _, err := s.CheckHistory(t.Context(), owner.ID, row.ID, "30d"); err != nil {
		t.Fatal(err)
	}
	t.Logf("30d history latency=%s", time.Since(start))
	if err := s.DB.Exec("UPDATE monitor_minutes SET minute_start = minute_start - interval '31 days'").Error; err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	var deleted int64
	for {
		count, err := s.PruneCheckHistory(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		deleted += count
		if count == 0 {
			break
		}
	}
	t.Log(fmt.Sprintf("retention cleanup: rows=%d elapsed=%s", deleted, time.Since(start)))
	if deleted != sizes.Rows {
		t.Fatal("retention cleanup left expired rows")
	}
}

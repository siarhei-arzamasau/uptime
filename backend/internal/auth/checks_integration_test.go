package auth_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"uptime-app/backend/internal/store"
)

func dueMonitor(t *testing.T, f *fixture, owner uuid.UUID) store.Monitor {
	t.Helper()
	m := store.Monitor{ID: uuid.New(), UserID: owner, URL: "https://example.com/health", IntervalSeconds: 5, CreatedAt: time.Now().UTC()}
	if err := f.s.CreateMonitor(t.Context(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func claimOne(t *testing.T, f *fixture) store.CheckJob {
	t.Helper()
	jobs, err := f.s.ClaimChecks(t.Context(), 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%v error=%v", jobs, err)
	}
	return jobs[0]
}

func finish(t *testing.T, f *fixture, job store.CheckJob, success bool) bool {
	t.Helper()
	status := 503
	if success {
		status = 200
	}
	now := time.Now().UTC()
	saved, err := f.s.FinishCheck(t.Context(), job, store.CheckResult{
		StartedAt: now, FinishedAt: now.Add(time.Millisecond), Success: success, HTTPStatus: &status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestCheckLeasesAndAtomicAccounting(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "checks@example.com")
	m := dueMonitor(t, f, owner.User.ID)
	var group sync.WaitGroup
	batches := make(chan []store.CheckJob, 2)
	for range 2 {
		group.Go(func() {
			jobs, err := f.s.ClaimChecks(context.Background(), 50)
			if err != nil {
				t.Error(err)
			}
			batches <- jobs
		})
	}
	group.Wait()
	close(batches)
	jobs := make([]store.CheckJob, 0)
	for batch := range batches {
		jobs = append(jobs, batch...)
	}
	if len(jobs) != 1 {
		t.Fatalf("multiple workers claimed %d jobs", len(jobs))
	}
	job := jobs[0]
	if !finish(t, f, job, true) || finish(t, f, job, true) {
		t.Fatal("result must be accounted once")
	}
	history, err := f.s.CheckHistory(t.Context(), owner.User.ID, m.ID, "1h")
	if err != nil || history.Successes != 1 || history.Failures != 0 || *history.Availability != 100 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	queued, err := f.s.ClaimChecks(t.Context(), 1)
	if err != nil || len(queued) != 0 {
		t.Fatal("finished job scheduled immediately")
	}
	// Lease expiry fences the original worker, then permits recovery with a new identity.
	if err := f.s.DB.Exec("UPDATE monitor_states SET next_check_at = now() - interval '1 hour' WHERE monitor_id = ?", m.ID).Error; err != nil {
		t.Fatal(err)
	}
	old := claimOne(t, f)
	if err := f.s.DB.Exec("UPDATE monitor_states SET lease_until = now() - interval '1 second' WHERE monitor_id = ?", m.ID).Error; err != nil {
		t.Fatal(err)
	}
	recovered := claimOne(t, f)
	if finish(t, f, old, false) || !finish(t, f, recovered, false) {
		t.Fatal("lease fencing failed")
	}
	var state store.MonitorState
	if err := f.s.DB.First(&state, "monitor_id = ?", m.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.NextCheckAt.Before(time.Now()) {
		t.Fatal("missed slots were replayed")
	}
}

func TestCheckEditsDeletionAndOwnership(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "owner-checks@example.com")
	other := f.register(t, "other-checks@example.com")
	m := dueMonitor(t, f, owner.User.ID)
	first := claimOne(t, f)
	if !finish(t, f, first, true) {
		t.Fatal("first check missing")
	}
	m.IntervalSeconds = 60
	if err := f.s.UpdateMonitor(t.Context(), &m); err != nil {
		t.Fatal(err)
	}
	old := claimOne(t, f)
	m.URL = "https://changed.example/health"
	if err := f.s.UpdateMonitor(t.Context(), &m); err != nil {
		t.Fatal(err)
	}
	jobs, err := f.s.ClaimChecks(t.Context(), 1)
	if err != nil || len(jobs) != 0 {
		t.Fatal("edit overlapped the outstanding lease")
	}
	if finish(t, f, old, true) {
		t.Fatal("old configuration accepted")
	}
	newJob := claimOne(t, f)
	if newJob.URL != m.URL {
		t.Fatal("new configuration not claimed")
	}
	history, err := f.s.CheckHistory(t.Context(), owner.User.ID, m.ID, "24h")
	if err != nil || history.Availability != nil {
		t.Fatal("URL edit did not clear history")
	}
	if !finish(t, f, newJob, false) {
		t.Fatal("new result missing")
	}
	m.IntervalSeconds = 5
	if err := f.s.UpdateMonitor(t.Context(), &m); err != nil {
		t.Fatal(err)
	}
	history, err = f.s.CheckHistory(t.Context(), owner.User.ID, m.ID, "24h")
	if err != nil || history.Failures != 1 {
		t.Fatal("interval edit erased history")
	}
	checkStatus(t, request(f.handler, "GET", "/api/v1/monitors/status?ids="+m.ID.String(), "", other.Access, nil, nil), 404)
	checkStatus(t, request(f.handler, "GET", "/api/v1/monitors/"+m.ID.String()+"/history", "", other.Access, nil, nil), 404)
	pending := claimOne(t, f)
	if err := f.s.DeleteMonitor(t.Context(), owner.User.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if finish(t, f, pending, true) {
		t.Fatal("deleted monitor revived")
	}
	var count int64
	if err := f.s.DB.Model(&store.MonitorMinute{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("history did not cascade")
	}
}

func TestCheckHistoryRetentionAndStatuses(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "history@example.com")
	m := dueMonitor(t, f, owner.User.ID)
	initial, err := f.s.MonitorStatuses(t.Context(), owner.User.ID, []uuid.UUID{m.ID})
	if err != nil || initial[0].Status != "pending" {
		t.Fatal("new monitor is not pending")
	}
	now := time.Now().UTC().Truncate(time.Minute)
	rows := []store.MonitorMinute{
		{MonitorID: m.ID, MinuteStart: now.Add(-2 * time.Minute), Successes: 9, Failures: 1},
		{MonitorID: m.ID, MinuteStart: now.Add(-time.Minute), Successes: 0, Failures: 1},
		{MonitorID: m.ID, MinuteStart: now.Add(-31 * 24 * time.Hour), Successes: 1000},
	}
	if err := f.s.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	history, err := f.s.CheckHistory(t.Context(), owner.User.ID, m.ID, "30d")
	if err != nil {
		t.Fatal(err)
	}
	if history.Successes != 9 || history.Failures != 2 || *history.Availability != 900.0/11.0 {
		t.Fatalf("wrong weighted totals: %+v", history)
	}
	if history.Buckets[0].Availability != nil {
		t.Fatal("empty bucket fabricated data")
	}
	if history.Buckets[len(history.Buckets)-1].End != history.To {
		t.Fatal("last bucket not clipped")
	}
	count, err := f.s.PruneCheckHistory(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("pruned %d: %v", count, err)
	}
	job := claimOne(t, f)
	if !finish(t, f, job, true) {
		t.Fatal("check missing")
	}
	if err := f.s.DB.Exec("UPDATE monitor_states SET last_started_at = now() - interval '21 seconds' WHERE monitor_id = ?", m.ID).Error; err != nil {
		t.Fatal(err)
	}
	response := request(f.handler, "GET", "/api/v1/monitors/status?ids="+m.ID.String(), "", owner.Access, nil, nil)
	checkStatus(t, response, 200)
	var payload struct {
		Monitors []store.MonitorStatus `json:"monitors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Monitors[0].Status != "stale" || payload.Monitors[0].LastSuccess == nil || !*payload.Monitors[0].LastSuccess {
		t.Fatal("stale snapshot lost previous result")
	}
	for _, path := range []string{"/api/v1/monitors/status", "/api/v1/monitors/status?ids=bad", "/api/v1/monitors/status?ids=" + m.ID.String() + "," + m.ID.String(), "/api/v1/monitors/" + m.ID.String() + "/history?period=bad"} {
		checkStatus(t, request(f.handler, "GET", path, "", owner.Access, nil, nil), 400)
	}
}

func TestMonitoringMigrationUpgrade(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "upgrade@example.com")
	db, err := f.s.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(db, ".", 4); err != nil {
		t.Fatal(err)
	}
	m := store.Monitor{ID: uuid.New(), UserID: owner.User.ID, URL: "https://example.com", IntervalSeconds: 1, CreatedAt: time.Now().UTC()}
	if err := f.s.DB.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := goose.Up(db, "."); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.s.DB.First(&m, "id = ?", m.ID).Error; err != nil {
		t.Fatal(err)
	}
	if m.IntervalSeconds != 5 {
		t.Fatal("legacy interval was not raised")
	}
	jobs, err := f.s.ClaimChecks(t.Context(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].MonitorID != m.ID {
		t.Fatal("existing monitor was not scheduled")
	}
	if !finish(t, f, jobs[0], true) {
		t.Fatal("upgraded monitor cannot save results")
	}
	if err := goose.DownTo(db, ".", 4); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.First(&m, "id = ?", m.ID).Error; err != nil || m.IntervalSeconds != 5 {
		t.Fatal("rollback damaged existing configuration")
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}
	history, err := f.s.CheckHistory(t.Context(), owner.User.ID, m.ID, "1h")
	if err != nil || history.Availability != nil {
		t.Fatal("rollback did not remove new history")
	}
}

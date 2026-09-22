package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"

	"uptime-app/backend/internal/store"
)

func TestMonitorCreationPersistenceAndOwnership(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "owner@example.com")
	other := f.register(t, "other@example.com")
	headers := map[string]string{"X-CSRF-Protection": "1"}
	for _, method := range []string{"GET", "POST"} {
		checkStatus(t, request(f.handler, method, "/api/v1/monitors", "", "", nil, headers), 401)
	}
	empty := request(f.handler, "GET", "/api/v1/monitors", "", owner.Access, nil, nil)
	checkStatus(t, empty, 200)
	if strings.TrimSpace(empty.Body.String()) != `{"monitors":[]}` {
		t.Fatal("empty list must be an array")
	}
	for _, interval := range []int{1, 5, 7, 60, 600, 3600, 7200, 2147483647} {
		body := fmt.Sprintf(`{"url":"  https://example.com/health  ","interval_seconds":%d}`, interval)
		created := request(f.handler, "POST", "/api/v1/monitors", body, owner.Access, nil, headers)
		checkStatus(t, created, 201)
		var m store.Monitor
		if err := json.Unmarshal(created.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.URL != "https://example.com/health" || m.IntervalSeconds != interval || m.ID.String() == "00000000-0000-0000-0000-000000000000" || m.CreatedAt.IsZero() {
			t.Fatalf("unexpected monitor: %+v", m)
		}
		if strings.Contains(created.Body.String(), "user_id") {
			t.Fatal("owner leaked in response")
		}
	}
	// Read through a new connection to prove creation was committed.
	reopened, err := store.Open(context.Background(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := reopened.MonitorsByUser(context.Background(), owner.User.ID, nil)
	if err != nil || len(rows) != 8 {
		t.Fatalf("persisted monitors: %d, error: %v", len(rows), err)
	}
	listed := request(f.handler, "GET", "/api/v1/monitors", "", owner.Access, nil, nil)
	checkStatus(t, listed, 200)
	var result struct {
		Monitors []store.Monitor `json:"monitors"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Monitors) != 8 || result.Monitors[0].IntervalSeconds != 2147483647 {
		t.Fatal("expected all monitors, newest first")
	}
	private := request(f.handler, "GET", "/api/v1/monitors", "", other.Access, nil, nil)
	checkStatus(t, private, 200)
	if strings.TrimSpace(private.Body.String()) != `{"monitors":[]}` {
		t.Fatal("another user's monitors were exposed")
	}
}

func TestMonitorRejectsInvalidRequests(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "owner@example.com")
	headers := map[string]string{"X-CSRF-Protection": "1"}
	valid := `{"url":"https://example.com","interval_seconds":7}`
	checkStatus(t, request(f.handler, "POST", "/api/v1/monitors", valid, owner.Access, nil, nil), 403)
	checkStatus(t, request(f.handler, "POST", "/api/v1/monitors", valid, owner.Access, nil, map[string]string{"Origin": "https://evil.example"}), 403)
	for _, body := range []string{
		`null`, `{}`, `[]`, `{"url":"https://example.com"}`, `{"url":null,"interval_seconds":7}`,
		`{"url":"https://127.0.0.999","interval_seconds":7}`, `{"url":"javascript:alert(1)","interval_seconds":7}`, `{"url":"https://user:secret@example.com","interval_seconds":7}`,
		`{"url":"https://example.com","interval_seconds":0}`, `{"url":"https://example.com","interval_seconds":-1}`,
		`{"url":"https://example.com","interval_seconds":1.5}`, `{"url":"https://example.com","interval_seconds":2147483648}`,
		`{"url":"https://example.com","interval_seconds":"7"}`, `{"url":"https://example.com","interval_seconds":7,"user_id":"other"}`,
		valid + `{}`, `{"url":"` + strings.Repeat("a", 17000) + `","interval_seconds":7}`,
	} {
		checkStatus(t, request(f.handler, "POST", "/api/v1/monitors", body, owner.Access, nil, headers), 400)
	}
	checkStatus(t, request(f.handler, "POST", "/api/v1/monitors", valid, owner.Access, nil, map[string]string{"X-CSRF-Protection": "1", "Content-Type": "text/plain"}), 400)
	rows, err := f.s.MonitorsByUser(context.Background(), owner.User.ID, nil)
	if err != nil || len(rows) != 0 {
		t.Fatal("invalid request persisted a monitor")
	}
}

func TestMonitorPagination(t *testing.T) {
	f := setup(t)
	owner := f.register(t, "pages@example.com")
	other := f.register(t, "other-pages@example.com")
	// Tied timestamps exercise the UUID tie-breaker and exact page boundaries.
	created := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 105; i++ {
		m := store.Monitor{ID: uuid.New(), UserID: owner.User.ID, URL: fmt.Sprintf("https://example.com/%d", i), IntervalSeconds: 7, CreatedAt: created}
		if err := f.s.CreateMonitor(context.Background(), &m); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Monitors   []store.Monitor `json:"monitors"`
		NextCursor string          `json:"next_cursor"`
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for index, size := range []int{50, 50, 5} {
		listed := request(f.handler, "GET", "/api/v1/monitors?cursor="+cursor, "", owner.Access, nil, nil)
		checkStatus(t, listed, 200)
		var result page
		if err := json.Unmarshal(listed.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Monitors) != size {
			t.Fatalf("page %d size: %d", index, len(result.Monitors))
		}
		for _, m := range result.Monitors {
			if seen[m.ID] {
				t.Fatal("duplicate row across pages")
			}
			seen[m.ID] = true
		}
		if index < 2 && result.NextCursor == "" {
			t.Fatal("missing next cursor")
		}
		cursor = result.NextCursor
		if index == 0 {
			// An insert ahead of the cursor must not shift or repeat subsequent pages.
			m := store.Monitor{ID: uuid.New(), UserID: owner.User.ID, URL: "https://new.example", IntervalSeconds: 1, CreatedAt: created.Add(time.Second)}
			if err := f.s.CreateMonitor(context.Background(), &m); err != nil {
				t.Fatal(err)
			}
			private := request(f.handler, "GET", "/api/v1/monitors?cursor="+cursor, "", other.Access, nil, nil)
			checkStatus(t, private, 200)
			if strings.TrimSpace(private.Body.String()) != `{"monitors":[]}` {
				t.Fatal("cursor bypassed ownership")
			}
		}
	}
	if cursor != "" || len(seen) != 105 {
		t.Fatal("incomplete pagination")
	}
	for _, invalid := range []string{"bad", "!", strings.Repeat("a", 129)} {
		checkStatus(t, request(f.handler, "GET", "/api/v1/monitors?cursor="+invalid, "", owner.Access, nil, nil), 400)
	}
}

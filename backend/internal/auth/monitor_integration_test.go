package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

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
	rows, err := reopened.MonitorsByUser(context.Background(), owner.User.ID)
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
		`{"url":"javascript:alert(1)","interval_seconds":7}`, `{"url":"https://user:secret@example.com","interval_seconds":7}`,
		`{"url":"https://example.com","interval_seconds":0}`, `{"url":"https://example.com","interval_seconds":-1}`,
		`{"url":"https://example.com","interval_seconds":1.5}`, `{"url":"https://example.com","interval_seconds":2147483648}`,
		`{"url":"https://example.com","interval_seconds":"7"}`, `{"url":"https://example.com","interval_seconds":7,"user_id":"other"}`,
		valid + `{}`, `{"url":"` + strings.Repeat("a", 17000) + `","interval_seconds":7}`,
	} {
		checkStatus(t, request(f.handler, "POST", "/api/v1/monitors", body, owner.Access, nil, headers), 400)
	}
	checkStatus(t, request(f.handler, "POST", "/api/v1/monitors", valid, owner.Access, nil, map[string]string{"X-CSRF-Protection": "1", "Content-Type": "text/plain"}), 400)
	rows, err := f.s.MonitorsByUser(context.Background(), owner.User.ID)
	if err != nil || len(rows) != 0 {
		t.Fatal("invalid request persisted a monitor")
	}
}

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPasswordGateConcurrentAdmission(t *testing.T) {
	g := newPasswordGate()
	var wg sync.WaitGroup
	releases := make(chan func(), 32)
	for range 32 {
		wg.Go(func() {
			release, err := g.acquire(context.Background())
			if err == nil {
				releases <- release
			} else if !errors.Is(err, ErrBusy) {
				t.Errorf("admission error: %v", err)
			}
		})
	}
	wg.Wait()
	close(releases)
	if len(releases) != passwordConcurrency {
		t.Fatalf("admitted %d operations, want %d", len(releases), passwordConcurrency)
	}
	for release := range releases {
		release()
	}
	release, err := g.acquire(context.Background())
	if err != nil {
		t.Fatalf("capacity was not released: %v", err)
	}
	release()
}

func TestPasswordGateRateAndCancellation(t *testing.T) {
	g := newPasswordGate()
	now := time.Now()
	g.last = now
	g.now = func() time.Time { return now }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	for range passwordBurst {
		release, err := g.acquire(context.Background())
		if err != nil {
			t.Fatalf("initial burst: %v", err)
		}
		release()
	}
	if _, err := g.acquire(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("rate limit: %v", err)
	}
	now = now.Add(time.Second / passwordRate)
	release, err := g.acquire(context.Background())
	if err != nil {
		t.Fatalf("rate limit did not refill: %v", err)
	}
	release()
	if _, err := g.acquire(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("refilled more than one admission: %v", err)
	}
}

func TestPasswordCapacityProtectsLoginAndRegistration(t *testing.T) {
	g := newPasswordGate()
	for range passwordConcurrency {
		release, err := g.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
	// No store or token signer: rejected requests must not reach hashing or DB work.
	s := &Service{passwords: g}
	mux := http.NewServeMux()
	NewController(s, nil, true).RegisterRoutes(mux)
	for _, action := range []string{"register", "login"} {
		r := httptest.NewRequest("POST", "/api/v1/auth/"+action, strings.NewReader(`{"email":"person@example.com","password":"correct long password"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" || len(w.Result().Cookies()) != 0 {
			t.Fatalf("%s: status=%d headers=%v", action, w.Code, w.Header())
		}
	}
	// Existing sessions are independent of the password gate.
	if err := s.Logout(context.Background(), ""); err != nil {
		t.Fatalf("logout must remain available: %v", err)
	}
}

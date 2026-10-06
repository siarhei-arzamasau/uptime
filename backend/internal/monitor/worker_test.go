package monitor

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"uptime-app/backend/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type closedBody struct {
	io.Reader
	closed bool
}

func (b *closedBody) Close() error { b.closed = true; return nil }

func TestWorkerCheck(t *testing.T) {
	for _, code := range []int{200, 201, 204, 301, 302, 400, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			worker, err := NewWorker(nil, 1)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			body := &closedBody{Reader: strings.NewReader("not consumed")}
			worker.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Error("not GET")
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Location": {"https://redirect.example"}}, Body: body, Request: r}, nil
			})
			result := worker.check(t.Context(), store.CheckJob{URL: "https://example.com"})
			if result.Success != (code == 200) || result.HTTPStatus == nil || *result.HTTPStatus != code || !body.closed || calls != 1 {
				t.Fatalf("incorrect check: %+v, closed=%v, calls=%d", result, body.closed, calls)
			}
		})
	}
	for _, tc := range []struct {
		name string
		err  error
		kind string
	}{
		{"DNS", &net.DNSError{Err: "not found", Name: "example.com"}, "dns"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"TLS", &tls.CertificateVerificationError{Err: errors.New("certificate")}, "tls"},
		{"private", errPrivateAddress, "blocked_address"},
		{"connection", errors.New("connection refused"), "connection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker, err := NewWorker(nil, 1)
			if err != nil {
				t.Fatal(err)
			}
			worker.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, tc.err })
			result := worker.check(t.Context(), store.CheckJob{URL: "https://example.com"})
			if result.Success || result.ErrorKind != tc.kind || result.HTTPStatus != nil {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestWorkerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, err := NewWorker(nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		worker.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		start := time.Now()
		result := worker.check(t.Context(), store.CheckJob{URL: "https://example.com"})
		if result.ErrorKind != "timeout" || time.Since(start) != checkTimeout {
			t.Fatal("request was not bounded by the full deadline")
		}
	})
}

type memoryChecks struct {
	mu       sync.Mutex
	jobs     []store.CheckJob
	saved    atomic.Int64
	released atomic.Int64
}

func (s *memoryChecks) ClaimChecks(_ context.Context, limit int) ([]store.CheckJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := min(limit, len(s.jobs))
	jobs := append([]store.CheckJob{}, s.jobs[:count]...)
	s.jobs = s.jobs[count:]
	return jobs, nil
}
func (s *memoryChecks) FinishCheck(context.Context, store.CheckJob, store.CheckResult) (bool, error) {
	s.saved.Add(1)
	return true, nil
}
func (s *memoryChecks) ReleaseCheck(context.Context, store.CheckJob) error {
	s.released.Add(1)
	return nil
}
func (s *memoryChecks) PruneCheckHistory(context.Context) (int64, error) { return 0, nil }

func TestWorkerBoundsConcurrencyAndCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, err := NewWorker(nil, 2)
		if err != nil {
			t.Fatal(err)
		}
		persistence := &memoryChecks{jobs: make([]store.CheckJob, 20)}
		for i := range persistence.jobs {
			persistence.jobs[i] = store.CheckJob{JobID: uuid.New(), URL: "https://example.com", ScheduledAt: time.Now()}
		}
		worker.store = persistence
		var started atomic.Int64
		worker.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			started.Add(1)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { worker.Run(ctx); close(done) }()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if started.Load() != 2 {
			t.Fatalf("started %d overlapping requests", started.Load())
		}
		cancel()
		<-done
		if persistence.saved.Load() != 0 || persistence.released.Load() != 2 || worker.active.Load() != 0 {
			t.Fatal("shutdown recorded a site failure or leaked active work")
		}
	})
}

type failingClaimChecks struct {
	memoryChecks
	claims atomic.Int64
}

func (s *failingClaimChecks) ClaimChecks(ctx context.Context, limit int) ([]store.CheckJob, error) {
	if s.claims.Add(1) == 1 {
		return append([]store.CheckJob{}, s.jobs...), errors.New("claim commit failed")
	}
	return s.memoryChecks.ClaimChecks(ctx, limit)
}

func TestWorkerDiscardsFailedClaimBatchAndRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		worker, err := NewWorker(nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		persistence := &failingClaimChecks{memoryChecks: memoryChecks{jobs: []store.CheckJob{
			{JobID: uuid.New(), URL: "https://example.com", ScheduledAt: time.Now()},
		}}}
		worker.store = persistence
		var requests atomic.Int64
		worker.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { worker.Run(ctx); close(done) }()
		synctest.Wait()
		if requests.Load() != 0 || persistence.saved.Load() != 0 || worker.active.Load() != 0 {
			t.Error("failed claim dispatched an unleased check")
		}
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		cancel()
		<-done
		if persistence.claims.Load() != 2 || requests.Load() != 1 || persistence.saved.Load() != 1 {
			t.Fatal("claim failure did not retry once at the normal polling interval")
		}
		if worker.active.Load() != 0 || persistence.released.Load() != 0 {
			t.Fatal("failed claim changed lease cleanup or leaked work")
		}
	})
}

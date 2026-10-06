package monitor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"uptime-app/backend/internal/store"
)

const checkTimeout = 10 * time.Second

type checkStore interface {
	ClaimChecks(context.Context, int) ([]store.CheckJob, error)
	FinishCheck(context.Context, store.CheckJob, store.CheckResult) (bool, error)
	ReleaseCheck(context.Context, store.CheckJob) error
	PruneCheckHistory(context.Context) (int64, error)
}

// Worker executes durable scheduled checks with bounded parallelism.
type Worker struct {
	store       checkStore
	client      *http.Client
	concurrency int
	active      atomic.Int64
	completed   atomic.Int64
	failed      atomic.Int64
	lagMillis   atomic.Int64
}

// NewWorker creates a public-network-only worker. Invalid parallelism returns an error; no I/O runs.
func NewWorker(s *store.Store, concurrency int) (*Worker, error) {
	if concurrency < 1 || concurrency > 4096 {
		return nil, fmt.Errorf("worker concurrency must be between 1 and 4096")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialPublicWithin(ctx, network, address, checkTimeout)
		},
		TLSHandshakeTimeout:    checkTimeout,
		ResponseHeaderTimeout:  checkTimeout,
		MaxResponseHeaderBytes: 32 * 1024,
		// Each observation performs fresh DNS validation and cannot inherit a stale pooled connection.
		DisableKeepAlives: true,
	}
	return &Worker{store: s, concurrency: concurrency, client: &http.Client{
		Transport: transport, Timeout: checkTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func checkError(err error) string {
	if errors.Is(err, errPrivateAddress) {
		return "blocked_address"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var certificate *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var authority x509.UnknownAuthorityError
	if errors.As(err, &certificate) || errors.As(err, &record) || errors.As(err, &authority) {
		return "tls"
	}
	return "connection"
}

func (w *Worker) check(ctx context.Context, job store.CheckJob) store.CheckResult {
	result := store.CheckResult{StartedAt: time.Now().UTC()}
	requestCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, job.URL, nil)
	if err != nil || !validURL(job.URL) {
		result.ErrorKind = "invalid_url"
		result.FinishedAt = time.Now().UTC()
		return result
	}
	req.Header.Set("User-Agent", "Uptime-Monitor/1.0")
	response, err := w.client.Do(req)
	if err != nil {
		result.ErrorKind = checkError(err)
	} else {
		result.HTTPStatus = &response.StatusCode
		result.Success = response.StatusCode == http.StatusOK
		if !result.Success {
			result.ErrorKind = "http_status"
		}
		// Availability concerns response headers, not body consumption or application content.
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.Debug("monitor response close failed", "monitor_id", job.MonitorID)
		}
	}
	result.FinishedAt = time.Now().UTC()
	return result
}

func (w *Worker) execute(ctx context.Context, job store.CheckJob) {
	defer w.active.Add(-1)
	lag := max(int64(0), time.Since(job.ScheduledAt).Milliseconds())
	for previous := w.lagMillis.Load(); lag > previous; previous = w.lagMillis.Load() {
		if w.lagMillis.CompareAndSwap(previous, lag) {
			break
		}
	}
	result := w.check(ctx, job)
	if ctx.Err() != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := w.store.ReleaseCheck(releaseCtx, job); err != nil {
			slog.Error("monitor lease release failed")
		}
		return
	}
	// Retrying this same fenced result is safe even if a commit acknowledgement was lost.
	for attempt := range 3 {
		saveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		saved, err := w.store.FinishCheck(saveCtx, job, result)
		cancel()
		if err == nil {
			if saved {
				w.completed.Add(1)
				if !result.Success {
					w.failed.Add(1)
				}
			}
			return
		}
		slog.Error("monitor result save failed", "monitor_id", job.MonitorID, "attempt", attempt+1)
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Run polls until ctx is cancelled and waits for requests/cleanup to exit.
// Transient database failures are logged and retried; no unbounded in-memory job queue is used.
func (w *Worker) Run(ctx context.Context) {
	var group sync.WaitGroup
	group.Go(func() { w.cleanup(ctx) })
	defer group.Wait()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	summary := time.NewTicker(time.Minute)
	defer summary.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		capacity := w.concurrency - int(w.active.Load())
		if capacity > 0 {
			claimCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			jobs, err := w.store.ClaimChecks(claimCtx, capacity)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Error("monitor claim failed")
			}
			for _, job := range jobs {
				w.active.Add(1)
				group.Go(func() { w.execute(ctx, job) })
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-summary.C:
			slog.Info("monitor worker summary", "active", w.active.Load(), "completed", w.completed.Swap(0),
				"failed", w.failed.Swap(0), "max_schedule_lag_ms", w.lagMillis.Swap(0))
		case <-ticker.C:
		}
	}
}

func (w *Worker) cleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil {
			batchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			count, err := w.store.PruneCheckHistory(batchCtx)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("monitor history cleanup failed")
				}
				break
			}
			if count == 0 {
				break
			}
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

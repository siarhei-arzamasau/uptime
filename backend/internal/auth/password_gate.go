package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrBusy = errors.New("authentication capacity exceeded")

const (
	passwordConcurrency = 4
	passwordBurst       = 8
	passwordRate        = 4 // Admissions per second, shared by login and registration.
)

// Bound expensive password work without queuing requests or keeping per-client
// state. The same gate protects every caller of a Service, including the BFF.
type passwordGate struct {
	mu     sync.Mutex
	active int
	tokens float64
	last   time.Time
	now    func() time.Time
}

func newPasswordGate() *passwordGate {
	return &passwordGate{tokens: passwordBurst, last: time.Now(), now: time.Now}
}

// acquire admits work immediately or returns ErrBusy/the existing context error.
// The caller must invoke the returned release function exactly once. Cancellation
// after admission does not release the slot or interrupt password hashing.
func (g *passwordGate) acquire(ctx context.Context) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := g.now()
	// Cap idle-time credit at the burst limit and ignore backwards clock movement.
	g.tokens = min(passwordBurst, g.tokens+max(0, now.Sub(g.last).Seconds())*passwordRate)
	g.last = now
	if g.active >= passwordConcurrency || g.tokens < 1 {
		return nil, ErrBusy
	}
	g.active++
	g.tokens--
	return func() {
		g.mu.Lock()
		g.active--
		g.mu.Unlock()
	}, nil
}

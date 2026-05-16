package auth

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

// ErrCircuitOpen is returned by the circuit breaker while it is tripped.
var ErrCircuitOpen = errors.New("github API circuit open: too many consecutive failures")

const (
	breakerFailureThreshold = 5
	breakerOpenDuration     = 60 * time.Second
)

type circuitBreaker struct {
	mu               sync.Mutex
	consecutiveFails int
	openedAt         time.Time
	clock            func() time.Time
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{clock: time.Now}
}

func (b *circuitBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return true
	}
	if b.clock().Sub(b.openedAt) >= breakerOpenDuration {
		// Half-open: allow a single probe.
		b.openedAt = time.Time{}
		b.consecutiveFails = breakerFailureThreshold - 1
		return true
	}
	return false
}

func (b *circuitBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFails = 0
	b.openedAt = time.Time{}
}

func (b *circuitBreaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFails++
	if b.consecutiveFails >= breakerFailureThreshold && b.openedAt.IsZero() {
		b.openedAt = b.clock()
	}
}

func isBreakable(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp.StatusCode >= 500
}

var apiBreaker = newCircuitBreaker()

// doGuarded routes the request through the package-level circuit breaker.
func doGuarded(req *http.Request) (*http.Response, error) {
	if !apiBreaker.allow() {
		return nil, ErrCircuitOpen
	}
	resp, err := httpClient.Do(req)
	if isBreakable(resp, err) {
		apiBreaker.recordFailure()
	} else {
		apiBreaker.recordSuccess()
	}
	return resp, err
}

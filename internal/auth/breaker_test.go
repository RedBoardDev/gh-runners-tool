package auth

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	b := newCircuitBreaker()
	for i := 0; i < breakerFailureThreshold; i++ {
		if !b.allow() {
			t.Fatalf("allow() = false before threshold, iteration %d", i)
		}
		b.recordFailure()
	}
	if b.allow() {
		t.Error("allow() = true after threshold, want false")
	}
}

func TestCircuitBreaker_SuccessResets(t *testing.T) {
	b := newCircuitBreaker()
	for i := 0; i < breakerFailureThreshold-1; i++ {
		b.recordFailure()
	}
	b.recordSuccess()
	if !b.allow() {
		t.Error("allow() = false after success reset")
	}
	for i := 0; i < breakerFailureThreshold-1; i++ {
		b.recordFailure()
	}
	if !b.allow() {
		t.Error("allow() = false below threshold")
	}
}

func TestCircuitBreaker_HalfOpenAfterTimeout(t *testing.T) {
	b := newCircuitBreaker()
	now := time.Now()
	b.clock = func() time.Time { return now }

	for i := 0; i < breakerFailureThreshold; i++ {
		b.recordFailure()
	}
	if b.allow() {
		t.Fatal("breaker should be open")
	}

	now = now.Add(breakerOpenDuration + time.Second)
	if !b.allow() {
		t.Error("breaker should allow a probe after open duration")
	}
}

func TestIsBreakable(t *testing.T) {
	if !isBreakable(nil, errors.New("net err")) {
		t.Error("network error should be breakable")
	}
	if !isBreakable(&http.Response{StatusCode: 503}, nil) {
		t.Error("5xx should be breakable")
	}
	if isBreakable(&http.Response{StatusCode: 401}, nil) {
		t.Error("4xx should not trip the breaker")
	}
	if isBreakable(&http.Response{StatusCode: 200}, nil) {
		t.Error("2xx should not trip the breaker")
	}
}

package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := map[string]time.Duration{
		"":                              0,
		"7":                             7 * time.Second,
		" 30 ":                          30 * time.Second,
		"0":                             0,
		"-5":                            0,
		"soon":                          0,
		"Fri, 23 Oct 2026 12:01:30 GMT": 90 * time.Second,
		"Fri, 23 Oct 2026 11:00:00 GMT": 0, // in the past
	}
	for in, want := range tests {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDelayJitterAndFloor(t *testing.T) {
	c := New(WithBackoffBase(time.Second))
	c.jitter = func() float64 { return 0 }
	if d := c.delay(1, 0); d != 500*time.Millisecond {
		t.Errorf("min jitter attempt 1 = %v", d)
	}
	if d := c.delay(3, 0); d != 2*time.Second {
		t.Errorf("min jitter attempt 3 = %v", d)
	}
	c.jitter = func() float64 { return 0.99 }
	if d := c.delay(1, 0); d < 1400*time.Millisecond || d >= 1500*time.Millisecond {
		t.Errorf("max jitter attempt 1 = %v", d)
	}
	// Retry-After is a floor, padded by up to 20 percent.
	c.jitter = func() float64 { return 0 }
	if d := c.delay(1, 10*time.Second); d != 10*time.Second {
		t.Errorf("retry-after floor = %v", d)
	}
	c.jitter = func() float64 { return 0.5 }
	if d := c.delay(1, 10*time.Second); d != 11*time.Second {
		t.Errorf("retry-after padded = %v", d)
	}
	// A short Retry-After never shortens the backoff.
	if d := c.delay(3, time.Millisecond); d != 4*time.Second {
		t.Errorf("backoff beats short retry-after = %v", d)
	}
}

func TestRetryAfterHonoredOn503(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	start := time.Now()
	body, err := testClient().Get(context.Background(), srv.URL)
	if err != nil || string(body) != "ok" {
		t.Fatalf("Get = %q, %v", body, err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("retried after %v, before the server's Retry-After of 1s", elapsed)
	}
}

func TestRetryAfterBeyondCapStops(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "3600")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	start := time.Now()
	_, err := testClient(WithMaxRetryAfter(time.Minute)).Get(context.Background(), srv.URL)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("calls = %d, want 1: a long Retry-After means go away", n)
	}
	if time.Since(start) > time.Second {
		t.Fatal("waited instead of giving up")
	}
}

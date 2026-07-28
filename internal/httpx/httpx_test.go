package httpx

import (
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// testClient returns a Client tuned for fast tests: millisecond backoff.
func testClient(opts ...Option) *Client {
	base := []Option{WithRetries(2), WithBackoffBase(time.Millisecond)}
	return New(append(base, opts...)...)
}

func TestGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		handler      func(calls *int32) http.HandlerFunc
		wantBody     string
		wantErr      error // sentinel matched with errors.Is; nil means success
		wantAnyErr   bool  // non-sentinel error expected
		wantAttempts int32
	}{
		{
			name: "200 returns body",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					w.Write([]byte(`{"ok":true}`))
				}
			},
			wantBody:     `{"ok":true}`,
			wantAttempts: 1,
		},
		{
			name: "gzip body is decoded",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					w.Header().Set("Content-Encoding", "gzip")
					gz := gzip.NewWriter(w)
					gz.Write([]byte("hello circus"))
					gz.Close()
				}
			},
			wantBody:     "hello circus",
			wantAttempts: 1,
		},
		{
			name: "404 returns ErrNotFound without retrying",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					http.Error(w, "no such board", http.StatusNotFound)
				}
			},
			wantErr:      ErrNotFound,
			wantAttempts: 1,
		},
		{
			name: "500 then 200 succeeds after retry",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if atomic.AddInt32(calls, 1) == 1 {
						http.Error(w, "boom", http.StatusInternalServerError)
						return
					}
					w.Write([]byte("recovered"))
				}
			},
			wantBody:     "recovered",
			wantAttempts: 2,
		},
		{
			name: "persistent 500 exhausts retries",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					http.Error(w, "boom", http.StatusInternalServerError)
				}
			},
			wantAnyErr:   true,
			wantAttempts: 3, // initial + 2 retries
		},
		{
			name: "429 then 200 succeeds after backoff",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if atomic.AddInt32(calls, 1) == 1 {
						http.Error(w, "slow down", http.StatusTooManyRequests)
						return
					}
					w.Write([]byte("patience pays"))
				}
			},
			wantBody:     "patience pays",
			wantAttempts: 2,
		},
		{
			name: "persistent 429 surfaces ErrRateLimited",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					http.Error(w, "slow down", http.StatusTooManyRequests)
				}
			},
			wantErr:      ErrRateLimited,
			wantAttempts: 3,
		},
		{
			name: "unexpected status (403) fails immediately",
			handler: func(calls *int32) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(calls, 1)
					http.Error(w, "forbidden", http.StatusForbidden)
				}
			},
			wantAnyErr:   true,
			wantAttempts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls int32
			srv := httptest.NewServer(tt.handler(&calls))
			defer srv.Close()

			body, err := testClient().Get(context.Background(), srv.URL)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Get() error = %v, want errors.Is(err, %v)", err, tt.wantErr)
				}
			case tt.wantAnyErr:
				if err == nil {
					t.Fatalf("Get() error = nil, want non-nil")
				}
			default:
				if err != nil {
					t.Fatalf("Get() unexpected error: %v", err)
				}
				if got := string(body); got != tt.wantBody {
					t.Fatalf("Get() body = %q, want %q", got, tt.wantBody)
				}
			}
			if got := atomic.LoadInt32(&calls); got != tt.wantAttempts {
				t.Fatalf("server saw %d attempts, want %d", got, tt.wantAttempts)
			}
		})
	}
}

func TestGetContextCancellation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := testClient().Get(ctx, srv.URL)
	if err == nil {
		t.Fatal("Get() error = nil, want context deadline error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Get() took %v, want prompt return on context deadline", elapsed)
	}
}

func TestGetDoesNotRetryAfterCancel(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled: no attempts should follow the first failure path

	_, err := testClient().Get(ctx, srv.URL)
	if err == nil {
		t.Fatal("Get() error = nil, want error for canceled context")
	}
	if got := atomic.LoadInt32(&calls); got > 1 {
		t.Fatalf("server saw %d attempts after cancel, want at most 1", got)
	}
}

func TestUserAgentSanitizedToASCII(t *testing.T) {
	t.Parallel()
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// The em-dash broke the Python original's latin-1 header encoding once.
	c := testClient(WithUserAgent("connie/1.0 — contact: joel@pushtomain.xyz"))
	if _, err := c.Get(context.Background(), srv.URL); err != nil {
		t.Fatalf("Get() unexpected error: %v", err)
	}
	if gotUA == "" {
		t.Fatal("server saw empty User-Agent")
	}
	for _, r := range gotUA {
		if r < 0x20 || r > 0x7e {
			t.Fatalf("User-Agent contains non-ASCII rune %q in %q", r, gotUA)
		}
	}
}

func TestHead(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		if r.URL.Path == "/gone" {
			http.Error(w, "", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := testClient()
	if err := c.Head(context.Background(), srv.URL+"/here"); err != nil {
		t.Fatalf("Head(existing) error = %v, want nil", err)
	}
	if err := c.Head(context.Background(), srv.URL+"/gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Head(missing) error = %v, want ErrNotFound", err)
	}
}

// Package httpx is connie's shared HTTP client: retry with exponential
// backoff, transparent gzip, sentinel errors for the statuses adapters care
// about, and an always-ASCII User-Agent (an em-dash in a UA header once broke
// the Python original's latin-1 encoding — the sanitizer is load-bearing).
package httpx

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotFound is returned for HTTP 404. Never retried: a missing board is a
// fact, not a fault.
var ErrNotFound = errors.New("not found")

// ErrRateLimited is returned when HTTP 429 persists through all retries.
var ErrRateLimited = errors.New("rate limited")

const (
	defaultRetries     = 2
	defaultTimeout     = 30 * time.Second
	defaultBackoffBase = time.Second
	defaultUserAgent   = "connie/1.0 (+https://github.com/joelrmartinez/connie)"
)

// Client is a retrying HTTP client for the ATS APIs.
type Client struct {
	hc          *http.Client
	retries     int
	backoffBase time.Duration
	ua          string
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout bounds each individual request attempt.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.hc.Timeout = d }
}

// WithRetries sets how many times a retryable failure (5xx, 429, transport
// error) is retried after the initial attempt.
func WithRetries(n int) Option {
	return func(c *Client) { c.retries = n }
}

// WithBackoffBase sets the first retry delay; subsequent delays double.
// Tests use a millisecond; production keeps the default second.
func WithBackoffBase(d time.Duration) Option {
	return func(c *Client) { c.backoffBase = d }
}

// WithUserAgent sets the User-Agent header. Non-ASCII runes are stripped.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.ua = sanitizeASCII(ua) }
}

// WithBaseTransport swaps the underlying RoundTripper (test seam).
func WithBaseTransport(rt http.RoundTripper) Option {
	return func(c *Client) { c.hc.Transport = rt }
}

// New builds a Client. The zero configuration is production-ready.
func New(opts ...Option) *Client {
	c := &Client{
		hc:          &http.Client{Timeout: defaultTimeout},
		retries:     defaultRetries,
		backoffBase: defaultBackoffBase,
		ua:          defaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get fetches url and returns the (gunzipped) body.
//
// Retry policy: 5xx, 429, and transport errors retry with exponential
// backoff; 404 returns ErrNotFound immediately; any other non-2xx status
// fails immediately. Context cancellation aborts both requests and waits.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, url)
}

// Head probes url with a HEAD request; nil means the resource exists.
// Same retry policy as Get, minus a body.
func (c *Client) Head(ctx context.Context, url string) error {
	_, err := c.do(ctx, http.MethodHead, url)
	return err
}

func (c *Client) do(ctx context.Context, method, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			if err := c.wait(ctx, attempt); err != nil {
				return nil, fmt.Errorf("%s %s: %w", method, url, err)
			}
		}

		body, retryable, err := c.attempt(ctx, method, url)
		if err == nil {
			return body, nil
		}
		if !retryable || ctx.Err() != nil {
			return nil, fmt.Errorf("%s %s: %w", method, url, err)
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%s %s after %d attempts: %w", method, url, c.retries+1, lastErr)
}

// attempt performs one request. retryable reports whether a failure is worth
// another try.
func (c *Client) attempt(ctx context.Context, method, url string) (body []byte, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, false, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, true, ErrRateLimited
	case resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("server error: %s", resp.Status)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, false, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	if method == http.MethodHead {
		return nil, false, nil
	}

	reader := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, true, fmt.Errorf("open gzip body: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	body, err = io.ReadAll(reader)
	if err != nil {
		return nil, true, fmt.Errorf("read body: %w", err)
	}
	return body, false, nil
}

// wait sleeps for the attempt's backoff delay or until ctx is done.
func (c *Client) wait(ctx context.Context, attempt int) error {
	delay := c.backoffBase << (attempt - 1) // 1x, 2x, 4x, ...
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sanitizeASCII strips runes outside printable ASCII so the header survives
// every proxy and logging pipeline between us and the ATS.
func sanitizeASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r <= 0x7e {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

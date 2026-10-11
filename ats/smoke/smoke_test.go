//go:build live

// Package smoke holds live-network sanity checks, excluded from normal runs.
//
//	go test -tags live ./internal/ats/smoke -v
//
// Three real requests, one tiny board per ATS. Set CONNIE_CONTACT first.
package smoke

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/joelrmartinez/connie/ats"
	"github.com/joelrmartinez/connie/ats/ashby"
	"github.com/joelrmartinez/connie/ats/greenhouse"
	"github.com/joelrmartinez/connie/ats/lever"
	"github.com/joelrmartinez/connie/internal/httpx"
)

func TestLiveFetchOneBoardPerATS(t *testing.T) {
	contact := os.Getenv("CONNIE_CONTACT")
	if contact == "" {
		t.Skip("set CONNIE_CONTACT to a real address before live runs")
	}
	client := httpx.New(httpx.WithUserAgent(
		fmt.Sprintf("connie-smoke/0.1 (+https://github.com/joelrmartinez/connie; contact: %s)", contact)))

	tests := []struct {
		adapter ats.Adapter
		board   string
	}{
		{ashby.New(client), "middesk"},
		{greenhouse.New(client), "cloudflare"},
		{lever.New(client), "palantir"},
	}
	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			jobs, err := tt.adapter.Fetch(ctx, tt.board, ats.FetchOptions{})
			if err != nil {
				t.Fatalf("live Fetch(%s/%s): %v", tt.adapter.Name(), tt.board, err)
			}
			if len(jobs) == 0 {
				t.Fatalf("live Fetch(%s/%s): 0 jobs — board empty or mapping broken", tt.adapter.Name(), tt.board)
			}
			t.Logf("%s/%s: %d postings; first: %q (%s, remote=%v)",
				tt.adapter.Name(), tt.board, len(jobs), jobs[0].Title, jobs[0].Location, jobs[0].Remote)
		})
	}
}

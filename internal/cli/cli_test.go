package cli

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saiwrd/connie/internal/discover"
	"github.com/saiwrd/connie/internal/store"
)

// rewriteTransport sends every request to the test server, whatever host
// the adapter asked for — the e2e seam.
type rewriteTransport struct{ target *url.URL }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "ats", rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return b
}

// atsServer plays all three ATS APIs from the recorded fixtures. Fixture
// bytes are read eagerly — handlers fire after setupDirs has chdir'd away.
func atsServer(t *testing.T) *httptest.Server {
	t.Helper()
	ashbyBody := fixture(t, "ashby/testdata/board.json")
	ghBody := fixture(t, "greenhouse/testdata/jobs.json")
	leverBody := fixture(t, "lever/testdata/postings.json")
	wkBody := fixture(t, "workable/testdata/account.json")

	mux := http.NewServeMux()
	mux.HandleFunc("/posting-api/job-board/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(ashbyBody)
	})
	mux.HandleFunc("/v1/boards/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(ghBody)
	})
	mux.HandleFunc("/v0/postings/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(leverBody)
	})
	mux.HandleFunc("/api/v1/widget/accounts/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(wkBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// setupDirs writes seed files for all three ATSes into a temp layout and
// chdirs there so relative default paths work.
func setupDirs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, ats := range []string{"ashby", "greenhouse", "lever", "workable"} {
		if err := discover.Save(filepath.Join(dir, "seeds", ats+".json"), []string{"testboard"}); err != nil {
			t.Fatalf("seed %s: %v", ats, err)
		}
	}
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir
}

func run(t *testing.T, srv *httptest.Server, args ...string) (int, string) {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	prev := testTransport
	testTransport = rewriteTransport{target: u}
	t.Cleanup(func() { testTransport = prev })

	var errBuf bytes.Buffer
	prevErr := stderr
	stderr = &errBuf
	t.Cleanup(func() { stderr = prevErr })

	return Run(args), errBuf.String()
}

func TestScanEndToEnd(t *testing.T) {
	srv := atsServer(t)
	dir := setupDirs(t)

	code, errOut := run(t, srv, "scan", "--all", "--out", "run", "--db", "test.db")
	if code != 0 {
		t.Fatalf("scan exit = %d, stderr:\n%s", code, errOut)
	}

	// CSV written: 2 ashby (unlisted dropped) + 2 greenhouse + 3 lever + 2 workable = 9 per board file.
	body, err := os.ReadFile(filepath.Join(dir, "run.csv"))
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\uFEFF")))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 1+9 {
		t.Fatalf("csv rows = %d, want header+9", len(records))
	}
	if _, err := os.Stat(filepath.Join(dir, "run.json")); err != nil {
		t.Fatalf("json missing: %v", err)
	}

	// Registry populated with the same 9.
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer s.Close()
	rows, err := s.Query(store.QueryOptions{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 9 {
		t.Fatalf("db rows = %d, want 9", len(rows))
	}
}

func TestScanTitleFilter(t *testing.T) {
	srv := atsServer(t)
	dir := setupDirs(t)

	code, errOut := run(t, srv, "scan", "--title", "platform engineer", "--out", "run", "--no-db")
	if code != 0 {
		t.Fatalf("scan exit = %d, stderr:\n%s", code, errOut)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "run.csv"))
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\uFEFF")))
	records, _ := r.ReadAll()
	// Only the synthetic lever "Staff Platform Engineer" matches.
	if len(records) != 2 {
		t.Fatalf("csv rows = %d, want header+1", len(records))
	}
}

func TestQueryReadsBackTheRegistry(t *testing.T) {
	srv := atsServer(t)
	setupDirs(t)
	if code, errOut := run(t, srv, "scan", "--all", "--db", "q.db", "--out", "run"); code != 0 {
		t.Fatalf("seed scan failed: %s", errOut)
	}

	var out bytes.Buffer
	prevOut := stdout
	stdout = &out
	t.Cleanup(func() { stdout = prevOut })

	if code, errOut := run(t, srv, "query", "--db", "q.db", "--open", "--format", "csv"); code != 0 {
		t.Fatalf("query exit != 0: %s", errOut)
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(out.String(), "\uFEFF")))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse query csv: %v", err)
	}
	if len(records) != 1+9 {
		t.Fatalf("query rows = %d, want header+9", len(records))
	}
}

func TestFlagValidation(t *testing.T) {
	srv := atsServer(t)
	setupDirs(t)
	tests := []struct {
		name string
		args []string
	}{
		{"no mode chosen", []string{"scan"}},
		{"all conflicts with title", []string{"scan", "--all", "--title", "x"}},
		{"all conflicts with grep", []string{"scan", "--all", "--grep", "x"}},
		{"bad grep pattern", []string{"scan", "--grep", "(unclosed"}},
		{"unknown ats", []string{"scan", "--all", "--ats", "workday"}},
		{"unknown subcommand", []string{"frobnicate"}},
		{"no subcommand", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code, _ := run(t, srv, tt.args...); code == 0 {
				t.Fatalf("Run(%v) = 0, want non-zero", tt.args)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	prevOut := stdout
	stdout = &out
	t.Cleanup(func() { stdout = prevOut })
	if code := Run([]string{"version"}); code != 0 {
		t.Fatalf("version exit = %d", code)
	}
	if !strings.Contains(out.String(), "connie") {
		t.Fatalf("version output = %q", out.String())
	}
}

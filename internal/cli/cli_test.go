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

	"github.com/joelrmartinez/connie/internal/discover"
	"github.com/joelrmartinez/connie/internal/store"
	"github.com/joelrmartinez/connie/internal/version"
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

func csvRecords(t *testing.T, path string) [][]string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return records
}

const testConfig = `titles: [Platform Engineer, Software Engineer]
location: {remote: us}
`

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "connie.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanUsesSavedSearch(t *testing.T) {
	srv := atsServer(t)
	dir := setupDirs(t)
	writeConfig(t, dir, testConfig)

	var out bytes.Buffer
	prevOut := stdout
	stdout = &out
	t.Cleanup(func() { stdout = prevOut })

	code, errOut := run(t, srv, "scan", "--out", "run", "--no-db")
	if code != 0 {
		t.Fatalf("scan exit = %d, stderr:\n%s", code, errOut)
	}
	// Ranked: Lever "Staff Platform Engineer" (remote US) first, Ashby
	// "Software Engineer" (remote, region unstated) next, and Workable's
	// onsite Greece role kept but last.
	records := csvRecords(t, filepath.Join(dir, "run.csv"))
	if len(records) != 1+3 {
		t.Fatalf("csv rows = %d, want header+3\n%s", len(records), errOut)
	}
	n := len(records[0])
	if records[0][n-2] != "score" || records[0][n-1] != "reason" {
		t.Fatalf("header = %v", records[0])
	}
	var titles []string
	for _, r := range records[1:] {
		titles = append(titles, r[3]+"="+r[n-2])
	}
	want := "Staff Platform Engineer (synthetic remote)=85.0|Software Engineer=80.0|Full-Stack Software Engineer=65.0"
	if got := strings.Join(titles, "|"); got != want {
		t.Errorf("ranked = %s\nwant     %s", got, want)
	}
	if !strings.Contains(records[3][n-1], "onsite Athens") || !strings.Contains(out.String(), "top 3 of 3") {
		t.Errorf("reason %q / stdout:\n%s", records[3][n-1], out.String())
	}

	// --hard-filter restores the strict filter: Greece is dropped.
	if code, errOut := run(t, srv, "scan", "--hard-filter", "--out", "hard", "--no-db"); code != 0 {
		t.Fatalf("scan: %s", errOut)
	}
	if got := len(csvRecords(t, filepath.Join(dir, "hard.csv"))); got != 1+2 {
		t.Fatalf("--hard-filter rows = %d, want header+2", got)
	}
}

func TestFlagsOverrideSavedSearch(t *testing.T) {
	srv := atsServer(t)
	dir := setupDirs(t)
	writeConfig(t, dir, testConfig+"ats: [lever]\n")

	// --title replaces the saved titles; the file still picks the ATS.
	if code, errOut := run(t, srv, "scan", "--title", "platform engineer", "--out", "a", "--no-db"); code != 0 {
		t.Fatalf("scan: %s", errOut)
	}
	if got := len(csvRecords(t, filepath.Join(dir, "a.csv"))); got != 2 {
		t.Fatalf("--title rows = %d, want header+1", got)
	}
	// --ats replaces the saved ATS list.
	if code, errOut := run(t, srv, "scan", "--all", "--ats", "workable", "--out", "b", "--no-db"); code != 0 {
		t.Fatalf("scan: %s", errOut)
	}
	if got := len(csvRecords(t, filepath.Join(dir, "b.csv"))); got != 1+2 {
		t.Fatalf("--ats rows = %d, want header+2", got)
	}
	// --no-config with no mode flag is the old error.
	if code, _ := run(t, srv, "scan", "--no-config", "--no-db"); code == 0 {
		t.Fatal("--no-config without a mode should fail")
	}
	// A named config that does not exist is an error.
	if code, _ := run(t, srv, "scan", "--config", "missing.yaml", "--no-db"); code == 0 {
		t.Fatal("missing --config should fail")
	}
}

func TestScanReportsPay(t *testing.T) {
	srv := atsServer(t)
	dir := setupDirs(t)
	code, errOut := run(t, srv, "scan", "--all", "--desc-pay", "--out", "run", "--no-db")
	if code != 0 {
		t.Fatalf("scan: %s", errOut)
	}
	if !strings.Contains(errOut, "unknown (flagged, kept)") {
		t.Fatalf("no pay summary in:\n%s", errOut)
	}
	records := csvRecords(t, filepath.Join(dir, "run.csv"))
	if len(records) != 1+9 || records[0][len(records[0])-4] != "pay_flag" {
		t.Fatalf("rows=%d header=%v", len(records), records[0])
	}
}

func TestContactWarning(t *testing.T) {
	var buf bytes.Buffer
	prev := stderr
	stderr = &buf
	t.Cleanup(func() { stderr = prev })

	t.Setenv("CONNIE_CONTACT", "")
	newClient()
	if !strings.Contains(buf.String(), "CONNIE_CONTACT is not set") {
		t.Fatalf("no warning when unset: %q", buf.String())
	}
	buf.Reset()
	t.Setenv("CONNIE_CONTACT", "you@example.com")
	newClient()
	if buf.Len() != 0 {
		t.Fatalf("unexpected warning when set: %q", buf.String())
	}
	if !strings.Contains(userAgent(), "contact: you@example.com") {
		t.Fatalf("userAgent() = %q", userAgent())
	}
}

func TestVersionCommand(t *testing.T) {
	var buf bytes.Buffer
	prev := stdout
	stdout = &buf
	t.Cleanup(func() { stdout = prev })
	if code := Run([]string{"version"}); code != 0 || !strings.Contains(buf.String(), version.Version) {
		t.Fatalf("version: code=%d out=%q", code, buf.String())
	}
}

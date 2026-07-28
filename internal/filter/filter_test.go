package filter

import (
	"strings"
	"testing"

	"github.com/saiwrd/connie/internal/ats"
)

// Ported table-for-table from the Python reference (test_ashby_jobs.py) —
// these semantics are load-bearing and deliberate.

func TestTitleMatchesFuzzy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title, query string
		want         bool
	}{
		// short query finds longer titles
		{"Senior Software Engineer, Backend", "software engineer", true},
		{"SOFTWARE ENGINEER II", "software engineer", true},
		// reverse: a long query still finds the short title inside it
		{"Software Engineer", "senior software engineer, backend", true},
		{"Engineering Manager", "software engineer", false},
		// the reverse direction must not drag in every one-word title
		{"Engineer", "senior software engineer", false},
		{"Software", "senior software engineer", false},
		{"Senior", "senior software engineer", false},
		{"Software Engineer", "senior software engineer", true},
		// empty either side matches nothing
		{"Chef", "", false},
		{"", "software engineer", false},
	}
	for _, tt := range tests {
		if got := TitleMatches(tt.title, tt.query, false); got != tt.want {
			t.Errorf("TitleMatches(%q, %q, fuzzy) = %v, want %v", tt.title, tt.query, got, tt.want)
		}
	}
}

func TestTitleMatchesExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		title, query string
		want         bool
	}{
		{"Software Engineer", "software engineer", true},
		{"  SOFTWARE ENGINEER  ", "software engineer", true},
		{"Senior Software Engineer, Backend", "software engineer", false},
		{"Software Engineer", "senior software engineer", false},
	}
	for _, tt := range tests {
		if got := TitleMatches(tt.title, tt.query, true); got != tt.want {
			t.Errorf("TitleMatches(%q, %q, exact) = %v, want %v", tt.title, tt.query, got, tt.want)
		}
	}
}

func TestPlainText(t *testing.T) {
	t.Parallel()
	in := "<p>We&nbsp;use <b>Go</b> &amp; Postgres.</p>\n\n  <li>a lot</li>"
	want := "We use Go & Postgres. a lot"
	if got := PlainText(in); got != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
}

func TestFragmentsGiveContextAndDedupe(t *testing.T) {
	t.Parallel()
	f, err := New(WithGrep("kubernetes"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	long := strings.Repeat("x", 200)
	text := "We run kubernetes in production. " + long +
		" More kubernetes here. " + long + " And kubernetes again — a third hit."

	job := ats.Job{Title: "SRE", Description: text}
	ok, frags := f.Match(job)
	if !ok {
		t.Fatal("Match() = false, want true")
	}
	// limit 2 even though there are 3 hits
	if len(frags) != 2 {
		t.Fatalf("fragments = %d, want 2", len(frags))
	}
	for _, fr := range frags {
		if !strings.Contains(strings.ToLower(fr), "kubernetes") {
			t.Errorf("fragment lacks the match: %q", fr)
		}
		// window ≈ 90 before + match + 150 after; generous ceiling
		if len(fr) > 90+len("kubernetes")+150+2 {
			t.Errorf("fragment too wide (%d chars): %q...", len(fr), fr[:40])
		}
	}
	if frags[0] == frags[1] {
		t.Error("fragments not deduped")
	}
}

func TestMatchTitleAndGrepBothRequired(t *testing.T) {
	t.Parallel()
	f, err := New(WithTitle("platform engineer", false), WithGrep("golang"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tests := []struct {
		name string
		job  ats.Job
		want bool
	}{
		{"both match", ats.Job{Title: "Senior Platform Engineer", Description: "we write golang"}, true},
		{"title only", ats.Job{Title: "Senior Platform Engineer", Description: "we write rust"}, false},
		{"grep only", ats.Job{Title: "Accountant", Description: "we write golang"}, false},
	}
	for _, tt := range tests {
		if ok, _ := f.Match(tt.job); ok != tt.want {
			t.Errorf("%s: Match() = %v, want %v", tt.name, ok, tt.want)
		}
	}
}

func TestMatchGrepSearchesDescriptionNotTitle(t *testing.T) {
	t.Parallel()
	f, err := New(WithGrep("golang"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The pattern appearing only in the title must not count — grep is a
	// description search (the Python reference's contract).
	if ok, _ := f.Match(ats.Job{Title: "Golang Engineer", Description: "we use java"}); ok {
		t.Error("grep matched on title, want description-only")
	}
}

func TestMatchRemoteOnly(t *testing.T) {
	t.Parallel()
	f, err := New(WithRemoteOnly())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ok, _ := f.Match(ats.Job{Title: "X", Remote: false}); ok {
		t.Error("onsite job passed a remote-only filter")
	}
	if ok, _ := f.Match(ats.Job{Title: "X", Remote: true}); !ok {
		t.Error("remote job failed a remote-only filter")
	}
}

func TestMatchGrepIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	f, err := New(WithGrep("kubernetes"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ok, _ := f.Match(ats.Job{Title: "X", Description: "We ship KUBERNETES daily"}); !ok {
		t.Error("grep not case-insensitive")
	}
}

func TestInvalidGrepFailsAtConstruction(t *testing.T) {
	t.Parallel()
	if _, err := New(WithGrep("(unclosed")); err == nil {
		t.Fatal("New() error = nil, want regex compile error at construction")
	}
}

func TestNeedsDescriptionsAndUnfiltered(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		opts       []Option
		needsDesc  bool
		unfiltered bool
	}{
		{"no filters", nil, false, true},
		{"title only", []Option{WithTitle("x y", false)}, false, false},
		{"grep", []Option{WithGrep("go")}, true, false},
		{"remote only", []Option{WithRemoteOnly()}, false, false},
	}
	for _, tt := range tests {
		f, err := New(tt.opts...)
		if err != nil {
			t.Fatalf("%s: New: %v", tt.name, err)
		}
		if got := f.NeedsDescriptions(); got != tt.needsDesc {
			t.Errorf("%s: NeedsDescriptions() = %v, want %v", tt.name, got, tt.needsDesc)
		}
		if got := f.Unfiltered(); got != tt.unfiltered {
			t.Errorf("%s: Unfiltered() = %v, want %v", tt.name, got, tt.unfiltered)
		}
	}
}

func TestUnfilteredMatchesEverything(t *testing.T) {
	t.Parallel()
	f, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ok, _ := f.Match(ats.Job{Title: "Anything At All"}); !ok {
		t.Error("unfiltered Match() = false, want true")
	}
}

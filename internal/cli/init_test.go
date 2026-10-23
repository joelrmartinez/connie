package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/joelrmartinez/connie/internal/config"
)

func runInitWith(t *testing.T, input string, args ...string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	prevIn, prevOut, prevErr := stdin, stdout, stderr
	var out bytes.Buffer
	stdin, stdout, stderr = strings.NewReader(input), &out, &out
	t.Cleanup(func() { stdin, stdout, stderr = prevIn, prevOut, prevErr })
	return Run(append([]string{"init"}, args...)), out.String()
}

func TestInitDefaults(t *testing.T) {
	code, out := runInitWith(t, "", "--defaults")
	if code != 0 {
		t.Fatalf("init --defaults = %d: %s", code, out)
	}
	c, err := config.Load("connie.yaml")
	if err != nil || c == nil {
		t.Fatalf("load written file: %v", err)
	}
	want := config.Defaults()
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("written config\n%+v\nwant\n%+v", c, want)
	}
	if c.Location.Remote != "us" || c.Location.Cities[0] != "Seattle" || !c.Location.Hybrid ||
		!c.Pay.CountDescription || c.Pay.Floor != 0 {
		t.Fatalf("defaults drifted from the decided ones: %+v", c)
	}
	assertAllFourATS(t, c)

	// A second run refuses to clobber without --force.
	if code := Run([]string{"init", "--defaults"}); code == 0 {
		t.Fatal("init over an existing file should fail")
	}
	if code := Run([]string{"init", "--defaults", "--force"}); code != 0 {
		t.Fatal("init --force should overwrite")
	}
}

func TestInitInteractive(t *testing.T) {
	answers := strings.Join([]string{
		"Account Executive, Solutions Engineer", // titles
		"mars",                                  // bad remote, asked again
		"anywhere",                              // remote
		"New York, Austin",                      // cities
		"maybe",                                 // bad yes/no
		"n",                                     // hybrid
		"150k",                                  // floor
		"",                                      // description pay: default yes
		"lever, workday",                        // unknown ats, asked again
		"lever,ashby",                           // ats
	}, "\n") + "\n"
	code, out := runInitWith(t, answers)
	if code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	c, err := config.Load("connie.yaml")
	if err != nil || c == nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Join(c.Titles, "|") != "Account Executive|Solutions Engineer" {
		t.Errorf("titles = %v", c.Titles)
	}
	if len(c.Variants["Account Executive"]) == 0 {
		t.Errorf("known variants not copied: %v", c.Variants)
	}
	if c.Location.Remote != "anywhere" || c.Location.Hybrid || len(c.Location.Cities) != 2 {
		t.Errorf("location = %+v", c.Location)
	}
	if c.Pay.Floor != 150000 || !c.Pay.CountDescription {
		t.Errorf("pay = %+v", c.Pay)
	}
	if strings.Join(c.ATS, ",") != "lever,ashby" {
		t.Errorf("ats = %v", c.ATS)
	}
	for _, msg := range []string{"please answer us, anywhere, or off", "please answer y or n", `unknown ats "workday"`} {
		if !strings.Contains(out, msg) {
			t.Errorf("output missing %q", msg)
		}
	}
}

// Blank answers all the way down (or a closed stdin) give the defaults.
func TestInitBlankAnswersTakeDefaults(t *testing.T) {
	if code, out := runInitWith(t, ""); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	c, _ := config.Load(filepath.Join(".", "connie.yaml"))
	if !reflect.DeepEqual(c, config.Defaults()) {
		t.Fatalf("got %+v", c)
	}
	assertAllFourATS(t, c)
}

// assertAllFourATS pins the default board list to a literal, so a shrunken
// AllATS or Defaults cannot pass by comparing against itself.
func assertAllFourATS(t *testing.T, c *config.Config) {
	t.Helper()
	want := []string{"ashby", "greenhouse", "lever", "workable"}
	if !reflect.DeepEqual(c.ATS, want) {
		t.Fatalf("default ats = %v, want %v", c.ATS, want)
	}
	raw, err := os.ReadFile("connie.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ats:\n  - ashby\n  - greenhouse\n  - lever\n  - workable\n") {
		t.Fatalf("connie.yaml does not list all four ATSes:\n%s", raw)
	}
}

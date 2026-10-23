package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joelrmartinez/connie/internal/discover"
)

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	return dir
}

func TestBoardsForEmbeddedFallback(t *testing.T) {
	chdirTemp(t)
	for _, name := range []string{"ashby", "greenhouse", "lever", "workable"} {
		boards, err := boardsFor(name, "")
		if err != nil {
			t.Fatalf("boardsFor(%s) from empty dir: %v", name, err)
		}
		if len(boards) == 0 {
			t.Fatalf("boardsFor(%s) = empty, want embedded seeds", name)
		}
	}
}

func TestBoardsForLocalOverrideWins(t *testing.T) {
	dir := chdirTemp(t)
	if err := discover.Save(filepath.Join(dir, "seeds", "ashby.json"), []string{"localonly"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	boards, err := boardsFor("ashby", "")
	if err != nil {
		t.Fatalf("boardsFor: %v", err)
	}
	if len(boards) != 1 || boards[0] != "localonly" {
		t.Fatalf("boards = %v, want [localonly]", boards)
	}
}

func TestBoardsForSeedsFlagWins(t *testing.T) {
	dir := chdirTemp(t)
	if err := discover.Save(filepath.Join(dir, "seeds", "lever.json"), []string{"local"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := discover.Save(filepath.Join(dir, "custom", "lever.json"), []string{"flagged"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	boards, err := boardsFor("lever", filepath.Join(dir, "custom"))
	if err != nil {
		t.Fatalf("boardsFor: %v", err)
	}
	if len(boards) != 1 || boards[0] != "flagged" {
		t.Fatalf("boards = %v, want [flagged]", boards)
	}
}

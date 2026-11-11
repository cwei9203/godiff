package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/godiff/internal/agent"
)

// TestWindowSweepsTheCacheOfItsOwn checks what a window sweeps as it opens:
// the reviews of each repository past the fifty it keeps, and the logs of
// the repairs past the twenty, in the directories the app writes them to.
func TestWindowSweepsTheCacheOfItsOwn(t *testing.T) {
	withStateFile(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := analysesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Sixty reviews of one repository, saved as the app saves them, the
	// last of them the newest.
	for i := range 60 {
		a := &agent.Analysis{Agent: "claude", Groups: []agent.Group{{Key: "code", Label: "Code", FilePaths: []string{"main.go"}}}}
		saveAnalysis(filepath.Join(dir, itoa(i)+".json"), "/repo/a", "key-"+itoa(i), a, nil)
	}
	runs := runsDir()
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		if err := os.WriteFile(filepath.Join(runs, "fix-"+itoa(i)+".log"), []byte("a run\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	w, _ := newTestWindow(t, testRepo(t))
	w.sweepCache()

	if got := len(namesIn(t, dir)); got != analysesKeptDefault {
		t.Errorf("%d reviews left, want the %d a repository keeps", got, analysesKeptDefault)
	}
	for i := 10; i < 60; i++ {
		if _, err := os.Stat(filepath.Join(dir, itoa(i)+".json")); err != nil {
			t.Errorf("the review %d, of the newest %d, is gone", i, analysesKeptDefault)
		}
	}
	if got := len(namesIn(t, runs)); got != runsKeptDefault {
		t.Errorf("%d logs left, want the %d kept", got, runsKeptDefault)
	}
	// A window opened on another repository sweeps nothing again.
	before := len(namesIn(t, dir))
	w.sweepCache()
	if got := len(namesIn(t, dir)); got != before {
		t.Errorf("%d reviews after the second sweep, %d after the first", got, before)
	}
}

// namesIn lists a directory.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name())
	}
	return out
}

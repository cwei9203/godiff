package gc

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// analyseSlot writes a review of a repository as the app saves it, older
// than the one after it.
func analyseSlot(t *testing.T, dir, root string, i, schema int) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("slot-%02d.json", i))
	body := fmt.Sprintf(`{"root":%q,"analysis":{"schemaVersion":%d},"fingerprints":{}}`, root, schema)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1_700_000_000+int64(i)*60, 0)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

// runLog writes a log of a repair, older than the one after it.
func runLog(t *testing.T, dir string, i int) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("fix-%02d.log", i))
	if err := os.WriteFile(path, []byte(fmt.Sprintf("run %d\n", i)), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1_700_000_000+int64(i)*60, 0)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

// names lists the files of a directory.
func names(t *testing.T, dir string) []string {
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

// TestSweepKeepsTheNewestOfEachRepository checks that each repository keeps
// its newest slots, and only its own: another repository's are no part of
// what this one may take.
func TestSweepKeepsTheNewestOfEachRepository(t *testing.T) {
	dir := t.TempDir()
	for i := range 60 {
		analyseSlot(t, dir, "/repo/a", i, 1)
	}
	// The other repository's, all older than the last of a's: they are
	// counted apart, and none of them goes.
	for i := range 25 {
		analyseSlot(t, dir, "/repo/b", 100+i, 1)
	}
	// The logs of the repairs, in a directory of their own, as the app
	// keeps them.
	runs := t.TempDir()
	for i := range 25 {
		runLog(t, runs, i)
	}
	r := Sweep(Options{Analyses: dir, Runs: runs, Slots: 50, Logs: 20, SchemaVersion: 1})
	if r.Analyses != 10 {
		t.Errorf("deleted %d reviews, want the 10 past the 50 of the repository", r.Analyses)
	}
	if r.Runs != 5 {
		t.Errorf("deleted %d logs, want the 5 past the 20 kept", r.Runs)
	}
	if r.Bytes == 0 {
		t.Error("no bytes freed, of the fifteen files deleted")
	}
	if kept := names(t, runs); len(kept) != 20 {
		t.Errorf("%d logs left, want 20", len(kept))
	}
	kept := names(t, dir)
	if len(kept) != 75 {
		t.Fatalf("%d reviews left, want the 50 of a and the 25 of b", len(kept))
	}
	// The newest of a stay, which are the ones written last.
	for i := 10; i < 60; i++ {
		name := fmt.Sprintf("slot-%02d.json", i)
		if !slices.Contains(kept, name) {
			t.Errorf("%s is gone, of the newest 50", name)
		}
	}
	for i := range 10 {
		name := fmt.Sprintf("slot-%02d.json", i)
		if slices.Contains(kept, name) {
			t.Errorf("%s stays, the oldest of 60", name)
		}
	}
}

// TestSweepKeepsTheNewestLogs checks the logs of the repairs alone: the
// newest 20 stay, whatever repository they were run in.
func TestSweepKeepsTheNewestLogs(t *testing.T) {
	dir := t.TempDir()
	for i := range 25 {
		runLog(t, dir, i)
	}
	r := Sweep(Options{Runs: dir, Logs: 20})
	if r.Runs != 5 {
		t.Errorf("deleted %d logs, want the 5 past the 20 kept", r.Runs)
	}
	kept := names(t, dir)
	if len(kept) != 20 {
		t.Fatalf("%d logs left, want 20", len(kept))
	}
	for i := 5; i < 25; i++ {
		name := fmt.Sprintf("fix-%02d.log", i)
		if !slices.Contains(kept, name) {
			t.Errorf("%s is gone, of the newest 20", name)
		}
	}
}

// TestSweepDeletesAnotherSchema checks a review of another contract, which
// the app would never read again: it goes, whatever place it holds.
func TestSweepDeletesAnotherSchema(t *testing.T) {
	dir := t.TempDir()
	for i := range 3 {
		analyseSlot(t, dir, "/repo/a", i, 2)
	}
	kept := analyseSlot(t, dir, "/repo/a", 3, 1)
	r := Sweep(Options{Analyses: dir, Slots: 50, SchemaVersion: 1})
	if r.Analyses != 3 {
		t.Errorf("deleted %d reviews, want the 3 of another schema", r.Analyses)
	}
	if left := names(t, dir); len(left) != 1 || left[0] != filepath.Base(kept) {
		t.Errorf("%v left, want the review of this schema alone", left)
	}
}

// TestSweepOfNoDirectory checks the directories a window finds nothing in:
// no review was ever saved, and no repair ever ran.
func TestSweepOfNoDirectory(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "never-made")
	r := Sweep(Options{Analyses: missing, Runs: missing, Slots: 50, Logs: 20, SchemaVersion: 1})
	if !r.Empty() {
		t.Errorf("%+v deleted of a directory that is not there", r)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Error("the sweep made the directory it found nothing in")
	}
	empty := t.TempDir()
	if r := Sweep(Options{Analyses: empty, Runs: empty, Slots: 50, Logs: 20, SchemaVersion: 1}); !r.Empty() {
		t.Errorf("%+v deleted of an empty directory", r)
	}
}

// TestSweepLeavesWhatItCannotRead checks a file it may not open: the sweep
// skips it rather than failing, or taking it for one of another schema.
func TestSweepLeavesWhatItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads what it is not allowed to")
	}
	dir := t.TempDir()
	unreadable := analyseSlot(t, dir, "/repo/a", 0, 1)
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o644) })
	for i := 1; i <= 3; i++ {
		analyseSlot(t, dir, "/repo/a", i, 1)
	}
	// Garbage where a review should be is nothing to sort either.
	junk := filepath.Join(dir, "junk.json")
	if err := os.WriteFile(junk, []byte("not a review"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Sweep(Options{Analyses: dir, Slots: 2, SchemaVersion: 1})
	if r.Analyses != 1 {
		t.Errorf("deleted %d reviews, want the one of the three past the 2 kept", r.Analyses)
	}
	if _, err := os.Stat(unreadable); err != nil {
		t.Errorf("the file the sweep may not read is gone: %v", err)
	}
	if _, err := os.Stat(junk); err != nil {
		t.Errorf("the file that is no review is gone: %v", err)
	}
}

// TestSweepOncePerDirectory checks that a second window on a repository
// sweeps nothing: the first one did.
func TestSweepOncePerDirectory(t *testing.T) {
	dir := t.TempDir()
	for i := range 3 {
		runLog(t, dir, i)
	}
	opts := Options{Runs: dir, Logs: 2}
	if _, ran := SweepOnce(opts); !ran {
		t.Fatal("the first sweep of a directory did not run")
	}
	if _, ran := SweepOnce(opts); ran {
		t.Error("the second sweep of a directory ran")
	}
	if kept := names(t, dir); len(kept) != 2 {
		t.Errorf("%d logs left, want the 2 the first sweep kept", len(kept))
	}
}

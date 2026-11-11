package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// tallRepo makes a repository of three files of many changed lines, so that
// a stack of them is taller than the window: the head of the last file is
// somewhere the surface has to scroll to.
func tallRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	paths := []string{"a.go", "b.go", "c.go"}
	lines := func(tag string) string {
		var b strings.Builder
		for i := range 80 {
			fmt.Fprintf(&b, "%s line %d\n", tag, i)
		}
		return b.String()
	}
	for _, path := range paths {
		writeFile(t, dir, path, lines("before"))
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")
	for _, path := range paths {
		writeFile(t, dir, path, lines("after"))
	}
	return dir
}

// newViewWindow opens the review of dir in the file view given, before its
// first frame: the position saved is restored as the window opens, and the
// file view it opens in is the one the first frame draws.
func newViewWindow(t *testing.T, dir, view string) (*window, *ui.Tester) {
	t.Helper()
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := newWindow(repo, source{})
	w.settings = defaultSettings()
	w.settings.FileView = view
	w.sidebarShown, w.sidebarWidth = true, sidebarDefault
	w.load()
	w.loadHistory()
	w.loadUser()
	tt := ui.NewTester(w.view, 1280, 860)
	tt.Frame()
	return w, tt
}

// withStateFile keeps the state of the tests in a file of their own, and
// puts back the one the other tests run with.
func withStateFile(t *testing.T) {
	t.Helper()
	path, data := state.path, state.data
	state.path, state.data = filepath.Join(t.TempDir(), "state.json"), storeData{}
	t.Cleanup(func() { state.path, state.data = path, data })
}

// TestPositionComesBackToOneFile checks the file shown last one file at a
// time: the next window opens on it, and the review is whole around it.
func TestPositionComesBackToOneFile(t *testing.T) {
	withStateFile(t)
	dir := fileViewRepo(t)
	w, _ := newViewWindow(t, dir, fileViewOne)
	if len(w.files) != 8 {
		t.Fatalf("%d files, want the eight the position is of", len(w.files))
	}
	// The sixth file is where the reader stopped, and closed the window
	// with it: stopped on at once, as the close writes it.
	w.current = 5
	want := w.files[5].Path
	w.flushPosition()
	key := positionKey(w.repo.Root, source{})
	if got := state.position(key); got != want {
		t.Fatalf("saved %q, want %q", got, want)
	}
	w2, tt := newViewWindow(t, dir, fileViewOne)
	shown := w2.shownFile()
	if shown < 0 || w2.files[shown].Path != want {
		t.Fatalf("the window opened on %q, want %q", w2.files[max(shown, 0)].Path, want)
	}
	if at, total := w2.fileNav(); at != 6 || total != 8 {
		t.Errorf("%d / %d, want the sixth file of the eight", at, total)
	}
	if !tt.HasText("6 / 8") {
		t.Errorf("no 6 / 8 in %q", tt.Texts())
	}
}

// TestPositionOfAFileGone checks a file deleted or renamed since: the
// surface starts at the first, and says nothing of it.
func TestPositionOfAFileGone(t *testing.T) {
	withStateFile(t)
	dir := fileViewRepo(t)
	w, _ := newViewWindow(t, dir, fileViewOne)
	state.setPosition(positionKey(w.repo.Root, source{}), "docs/gone.md")
	w2, _ := newViewWindow(t, dir, fileViewOne)
	if shown := w2.shownFile(); shown != 0 {
		t.Errorf("the window opened on %q, want the first file", w2.files[shown].Path)
	}
}

// TestPositionOfTheStack checks the same with every file stacked: the
// surface opens at the head of the file shown last, not at its own top.
func TestPositionOfTheStack(t *testing.T) {
	withStateFile(t)
	// A stack taller than the window, so that the head of a file below the
	// first is somewhere the surface had to scroll to.
	dir := tallRepo(t)
	w, _ := newViewWindow(t, dir, fileViewStack)
	last := len(w.files) - 1
	w.current = last
	want := w.files[last].Path
	w.flushPosition()
	w2, _ := newViewWindow(t, dir, fileViewStack)
	first, _ := w2.list.Visible()
	if first < 1 {
		t.Fatalf("the surface opens at row %d, the top of the stack", first)
	}
	if first >= len(w2.rows) {
		t.Fatalf("no row %d, of %d", first, len(w2.rows))
	}
	top := w2.rows[first]
	if top.kind != rowHeader || w2.files[top.file].Path != want {
		t.Errorf("the surface opens at %v of %q, want the head of %q", top.kind, w2.files[top.file].Path, want)
	}
}

// TestPositionWaitsForTheFileToStay checks the wait before the file shown
// is saved: a walk through the files saves the last one alone.
func TestPositionWaitsForTheFileToStay(t *testing.T) {
	withStateFile(t)
	dir := fileViewRepo(t)
	w, _ := newViewWindow(t, dir, fileViewOne)
	key := positionKey(w.repo.Root, source{})
	// A file that stays is saved.
	first := w.files[0].Path
	if got := state.position(key); got != first {
		t.Fatalf("saved %q of the file opened on, want %q", got, first)
	}
	// A walk through them waits: what the walk left is dropped, and the
	// file it stopped on is the one saved.
	w.hold = true
	w.current = 2
	w.rememberPosition()
	w.current = 5
	w.rememberPosition()
	if got := state.position(key); got != first {
		t.Errorf("saved %q before the file stayed, want %q", got, first)
	}
	for len(w.held) > 0 {
		fn := w.held[0]
		w.held = w.held[1:]
		fn()
	}
	if got, want := state.position(key), w.files[5].Path; got != want {
		t.Errorf("saved %q, want %q: the last file alone", got, want)
	}
}

// TestPositionOfASourceOfItsOwn checks that each source keeps its place: the
// file shown in a commit says nothing of the work tree's.
func TestPositionOfASourceOfItsOwn(t *testing.T) {
	withStateFile(t)
	dir := fileViewRepo(t)
	w, _ := newViewWindow(t, dir, fileViewOne)
	w.current = 5
	workTree := positionKey(w.repo.Root, source{})
	commit := positionKey(w.repo.Root, source{kind: sourceCommit, ref: "0123456789abcdef"})
	if workTree == commit {
		t.Fatalf("a commit and the work tree share the key %q", workTree)
	}
	w.flushPosition()
	if got := state.position(commit); got != "" {
		t.Errorf("the commit has the position %q, of the work tree's file", got)
	}
	if got := state.position(workTree); got != w.files[5].Path {
		t.Errorf("the work tree has %q, want %q", got, w.files[5].Path)
	}
}

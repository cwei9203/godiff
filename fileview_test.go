package main

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// fileViewRepo makes a repository of eight files in three kinds, so that a
// file of the file view has a place in the whole and a place in its group.
func fileViewRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	paths := []string{
		"docs/a.md", "docs/b.md", "docs/c.md",
		"main.go", "other.go", "util.go",
		"main_test.go", "util_test.go",
	}
	for _, path := range paths {
		writeFile(t, dir, path, "before\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")
	for _, path := range paths {
		writeFile(t, dir, path, "after\n")
	}
	return dir
}

// fileViewWindow opens the review of fileViewRepo with one file at a time,
// grouped by kind: docs, then code, then tests.
func fileViewWindow(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	w, tt := newTestWindow(t, fileViewRepo(t))
	w.groupPref = groupKind
	w.settings.FileView = fileViewOne
	w.regroup()
	tt.Frame()
	if got := len(w.files); got != 8 {
		t.Fatalf("%d files, want the eight the numbering counts", got)
	}
	return w, tt
}

// keepFileView keeps the file view of the settings as the test leaves it,
// and puts back the one the other tests run with.
func keepFileView(t *testing.T) {
	t.Helper()
	old := cfg.Get().FileView
	t.Cleanup(func() { cfg.Update(func(s *Settings) { s.FileView = old }) })
}

// TestFileViewOneLaysOutOneFile checks that one file at a time lays out that
// file's rows alone: the files beside it add none of their own.
func TestFileViewOneLaysOutOneFile(t *testing.T) {
	w, _ := fileViewWindow(t)
	// The third file of the eight.
	w.current = 2
	w.buildRows()
	one := w.rows
	for i := range one {
		r := &one[i]
		if r.kind == rowFileNav || r.banner() {
			continue
		}
		if int(r.file) != w.current {
			t.Errorf("a row of %s shows, of the file %s", w.files[r.file].Path, w.files[w.current].Path)
		}
	}
	if got := countRows(w, rowHeader); got != 1 {
		t.Errorf("%d file cards, want 1", got)
	}
	if got := countRows(w, rowFileNav); got != 1 {
		t.Errorf("%d navigation bars, want 1", got)
	}
	// The same rows without the files beside it: none of them reached the
	// surface to begin with.
	w.filter = w.files[w.current].Path
	w.buildRows()
	if got := len(w.rows); got != len(one) {
		t.Errorf("%d rows with the other files filtered out, %d with them", got, len(one))
	}
	w.filter = ""
}

// TestFileViewStackShowsEveryFile checks the other view: every file, one
// under the other, and no navigation bar.
func TestFileViewStackShowsEveryFile(t *testing.T) {
	w, tt := fileViewWindow(t)
	keepFileView(t)
	w.setFileView(fileViewStack)
	tt.Frame()
	if w.oneFile() {
		t.Fatal("the stack still shows one file")
	}
	w.buildRows()
	if got := countRows(w, rowHeader); got != len(w.files) {
		t.Errorf("%d file cards, want %d", got, len(w.files))
	}
	if got := countRows(w, rowFileNav); got != 0 {
		t.Errorf("%d navigation bars, want none", got)
	}
	if !tt.HasText(textView) {
		t.Errorf("the toolbar has no View menu: %q", tt.Texts())
	}
}

// TestFileViewMenuSwitches checks the View menu's file view: the item says
// which view it goes to, and the click shows it, in the window and in the
// settings both.
func TestFileViewMenuSwitches(t *testing.T) {
	w, tt := fileViewWindow(t)
	keepFileView(t)
	tt.Frame()
	if err := tt.Click(textView); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), textStack) {
		t.Fatalf("the View menu has no file view: %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem(textStack); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.oneFile() || cfg.Get().FileView != fileViewStack {
		t.Errorf("one file %v, the settings say %q", w.oneFile(), cfg.Get().FileView)
	}
	if err := tt.Click(textView); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(textOneFile); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.oneFile() || cfg.Get().FileView != fileViewOne {
		t.Errorf("one file %v, the settings say %q", w.oneFile(), cfg.Get().FileView)
	}
}

// TestFileViewNavNumbers checks where the file shown stands: it numbers
// every file of the review, since the file view shows every one of them.
func TestFileViewNavNumbers(t *testing.T) {
	w, tt := fileViewWindow(t)
	// The third file of the eight.
	w.current = 2
	tt.Frame()
	if at, total := w.fileNav(); at != 3 || total != 8 {
		t.Errorf("%d / %d, want 3 / 8", at, total)
	}
	if !tt.HasText("3 / 8") {
		t.Errorf("no 3 / 8 in %q", tt.Texts())
	}
	// A group's way in shows its first file, and numbers that file among
	// every file.
	w.showGroup(groupIndex(t, w, "code"))
	tt.Frame()
	if path := w.files[w.current].Path; path != "main.go" {
		t.Errorf("the group opens on %s, want its first file", path)
	}
	if at, total := w.fileNav(); at != 4 || total != 8 {
		t.Errorf("%d / %d, want 4 / 8", at, total)
	}
	if !tt.HasText("4 / 8") {
		t.Errorf("no 4 / 8 in %q", tt.Texts())
	}
}

// TestFileViewNavEnds checks the ends of the file view: nothing before the
// first file, nothing after the last, and the buttons there are off.
func TestFileViewNavEnds(t *testing.T) {
	w, tt := fileViewWindow(t)
	tt.Frame()
	if w.canStep(-1) {
		t.Error("a file comes before the first")
	}
	w.stepFile(-1)
	if w.current != 0 {
		t.Errorf("the step before the first showed %s", w.files[w.current].Path)
	}
	if err := tt.Click("Previous file ([)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.current != 0 {
		t.Errorf("the button before the first showed %s", w.files[w.current].Path)
	}
	if !tt.HasText("1 / 8") {
		t.Errorf("no 1 / 8 in %q", tt.Texts())
	}
	w.current = len(w.files) - 1
	tt.Frame()
	if w.canStep(1) {
		t.Error("a file comes after the last")
	}
	w.stepFile(1)
	if w.current != len(w.files)-1 {
		t.Errorf("the step past the last showed %s", w.files[w.current].Path)
	}
	if err := tt.Click("Next file (])"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.current != len(w.files)-1 {
		t.Errorf("the button past the last showed %s", w.files[w.current].Path)
	}
	if !tt.HasText("8 / 8") {
		t.Errorf("no 8 / 8 in %q", tt.Texts())
	}
}

// TestFileViewKeysStep checks [ and ], which show the file before and the
// one after.
func TestFileViewKeysStep(t *testing.T) {
	w, tt := fileViewWindow(t)
	tt.Frame()
	first := w.files[w.current].Path
	tt.Key(0, ui.KeyBracketRight)
	tt.Frame()
	if w.current != 1 {
		t.Fatalf("] showed the file at %d, want 1", w.current)
	}
	tt.Key(0, ui.KeyBracketRight)
	tt.Frame()
	if w.current != 2 {
		t.Fatalf("] showed the file at %d, want 2", w.current)
	}
	tt.Key(0, ui.KeyBracketLeft)
	tt.Frame()
	if w.current != 1 {
		t.Fatalf("[ showed the file at %d, want 1", w.current)
	}
	// The rows are the ones of the file shown, and its own name heads them.
	if got := countRows(w, rowHeader); got != 1 {
		t.Errorf("%d file cards, want 1", got)
	}
	if !tt.HasText(w.files[1].Path) {
		t.Errorf("the file shown is not named: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyBracketLeft)
	tt.Frame()
	if w.files[w.current].Path != first {
		t.Errorf("[ showed %s, want %s", w.files[w.current].Path, first)
	}
}

// TestFileViewSummaryFolds checks the agent's review in the file view: it
// tells of every change, not of the file shown, so it folds to a line that
// opens it.
func TestFileViewSummaryFolds(t *testing.T) {
	w, tt := fileViewWindow(t)
	if _, ok := agent.ByName("pi"); !ok {
		t.Skip("no Pi among the agents")
	}
	st := w.analysisFor(w.source)
	st.result = &agent.Analysis{
		OverallSummary: "The changes range wide.",
		Agent:          "pi",
		Model:          "sonnet",
		GeneratedAt:    time.Now(),
		Groups:         []agent.Group{{Key: "all", Label: "Everything", FilePaths: []string{"main.go"}}},
	}
	w.regroup()
	w.current = 0
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	line, ok := tt.Find("AI Review · Pi · sonnet · just now")
	if !ok {
		t.Fatalf("no folded line of the review in %q", tt.Texts())
	}
	if tt.HasText("The changes range wide.") {
		t.Error("the card shows whole in the file view")
	}
	// The line opens the card again.
	tt.ClickAt(line.X+line.W/2, line.Y+line.H/2)
	tt.Frame()
	if !w.summaryOpen || !tt.HasText("The changes range wide.") {
		t.Fatalf("the line did not open the card: open %v, %q", w.summaryOpen, tt.Texts())
	}
}

// TestFileViewSelectsFromTheTree checks a file of the Files section: it
// shows, the navigation bar numbering it among every file.
func TestFileViewSelectsFromTheTree(t *testing.T) {
	w, tt := fileViewWindow(t)
	if err := tt.Click("util.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if path := w.files[w.current].Path; path != "util.go" {
		t.Fatalf("%s shows, want util.go", path)
	}
	if at, total := w.fileNav(); at != 6 || total != 8 {
		t.Errorf("%d / %d, want 6 / 8", at, total)
	}
	if got := countRows(w, rowHeader); got != 1 {
		t.Errorf("%d file cards, want 1", got)
	}
	// A second click shows the same file again: it chooses no more.
	if err := tt.Click("util.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if path := w.files[w.current].Path; path != "util.go" {
		t.Errorf("the second click showed %s, want util.go", path)
	}
}

// TestFileViewSnapshots saves the three frames of the file view, to look at.
func TestFileViewSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	one := func(width, height int) (*window, *ui.Tester) {
		w, tt := newTestWindow(t, fileViewRepo(t))
		tt.SetSize(width, height)
		w.groupPref = groupKind
		w.settings.FileView = fileViewOne
		w.regroup()
		w.current = 3
		tt.Frame()
		tt.Frame()
		return w, tt
	}
	// One file at a time, a window wide enough for the sidebar and the
	// diffs beside it.
	_, tt := one(1600, 1000)
	snapshot(t, tt, "fileview-one")
	// The same, narrower.
	_, tt = one(1280, 860)
	snapshot(t, tt, "fileview-one-narrow")
	// Every file, one under the other, from the top of the review.
	w, tt := newTestWindow(t, fileViewRepo(t))
	w.groupPref = groupKind
	w.regroup()
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "fileview-stack")
}

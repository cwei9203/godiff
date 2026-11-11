package main

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// kindRepo makes a repository whose changes fall into six groups: docs,
// code, tests, config, deps, and generated. Six groups tell a fold apart
// from the row beside it, where a repository of one kind of file would not.
func kindRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	files := map[string]string{
		"README.md":                "# groups\n",
		"app.go":                   "package app\n\nfunc Run() {}\n",
		"app_test.go":              "package app\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n",
		".github/workflows/ci.yml": "name: ci\non: push\n",
		"go.mod":                   "module groups\n\ngo 1.24\n",
		"dist/bundle.js":           "// built\n",
	}
	for path, content := range files {
		writeFile(t, dir, path, content)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")
	changed := map[string]string{
		"README.md":                "# groups\n\nThe fold of the group, not a selection.\n",
		"app.go":                   "package app\n\nfunc Run() { println(\"run\") }\n",
		"app_test.go":              "package app\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) { t.Log(1) }\n",
		".github/workflows/ci.yml": "name: ci\non: [push, pull_request]\n",
		"go.mod":                   "module groups\n\ngo 1.24\n\nrequire example.com/dep v1.2.3\n",
		"dist/bundle.js":           "// built\nconsole.log(1);\n",
	}
	for path, content := range changed {
		writeFile(t, dir, path, content)
	}
	return dir
}

// kindWindow opens the review of kindRepo grouped by kind, every file
// stacked.
func kindWindow(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	w, tt := newTestWindow(t, kindRepo(t))
	w.groupPref = groupKind
	w.regroup()
	tt.Frame()
	if len(w.groups) != 6 {
		t.Fatalf("%d groups, want the six the kinds make", len(w.groups))
	}
	return w, tt
}

// groupIndex finds a group of a window by its key.
func groupIndex(t *testing.T, w *window, key string) int {
	t.Helper()
	gi := slices.IndexFunc(w.groups, func(g fileGroup) bool { return g.key == key })
	if gi < 0 {
		t.Fatalf("no group %q in %v", key, w.groups)
	}
	return gi
}

// fileIndex finds a file of a window by its path.
func fileIndex(t *testing.T, w *window, path string) int {
	t.Helper()
	i := slices.IndexFunc(w.files, func(f *fileState) bool { return f.Path == path })
	if i < 0 {
		t.Fatalf("no file %q in %d files", path, len(w.files))
	}
	return i
}

// TestStackLaysOutEveryFile checks the stacked view: every file's card,
// under the band of its group.
func TestStackLaysOutEveryFile(t *testing.T) {
	w, _ := kindWindow(t)
	w.buildRows()
	if got := countRows(w, rowHeader); got != len(w.files) {
		t.Errorf("%d file cards, want %d", got, len(w.files))
	}
	if got := countRows(w, rowGroup); got != len(w.groups) {
		t.Errorf("%d group bands, want %d", got, len(w.groups))
	}
}

// TestGroupRowFoldsOnTheWholeRow checks the group's row: a click anywhere on
// it folds its files away, a second click opens them again, and neither
// click moves the surface off the file it showed nor takes the focus from
// the diffs.
func TestGroupRowFoldsOnTheWholeRow(t *testing.T) {
	w, tt := kindWindow(t)
	shown := w.files[w.current].Path
	if !tt.Focused("Changes") {
		t.Fatal("the diffs do not start with the focus")
	}
	// The row's name, not the chevron: the whole row folds the group.
	if err := tt.Click("Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.closedDirs["g:docs"] {
		t.Fatalf("the click did not fold Docs: %v", w.closedDirs)
	}
	if w.inFiles("README.md") {
		t.Error("the folded group still lists its file")
	}
	if got := w.files[w.current].Path; got != shown {
		t.Errorf("the fold showed %s, want %s still", got, shown)
	}
	if !tt.Focused("Changes") {
		t.Error("the fold took the focus from the diffs")
	}
	// A second click opens it again, the same way.
	if err := tt.Click("Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.closedDirs["g:docs"] || !w.inFiles("README.md") {
		t.Fatalf("the second click did not open Docs: %v", w.closedDirs)
	}
	if got := w.files[w.current].Path; got != shown {
		t.Errorf("opening the group showed %s, want %s still", got, shown)
	}
	if !tt.Focused("Changes") {
		t.Error("opening the group took the focus from the diffs")
	}
}

// TestGroupRowFoldsOnTheChevron checks the chevron: it says which way the
// fold goes, and a click on it is the row's one fold, not a second action of
// its own.
func TestGroupRowFoldsOnTheChevron(t *testing.T) {
	w, tt := kindWindow(t)
	if err := tt.Click("Hide the files of Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.closedDirs["g:docs"] {
		t.Fatalf("the chevron did not fold Docs: %v", w.closedDirs)
	}
	if err := tt.Click("Show the files of Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.closedDirs["g:docs"] {
		t.Fatalf("the chevron did not open Docs again: %v", w.closedDirs)
	}
}

// TestGroupRowKeepsTheFileShownAlone checks the fold in the file view: the
// card the surface shows stays the one it showed, even one of another group.
func TestGroupRowKeepsTheFileShownAlone(t *testing.T) {
	w, tt := kindWindow(t)
	keepFileView(t)
	w.setFileView(fileViewOne)
	// A file of Config, while the click folds Code: a fold that went to the
	// group would show app.go instead.
	w.current = fileIndex(t, w, ".github/workflows/ci.yml")
	tt.Frame()
	shown := w.shownFile()
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.closedDirs["g:code"] {
		t.Fatalf("the click did not fold Code: %v", w.closedDirs)
	}
	if got := w.shownFile(); got != shown {
		t.Errorf("the fold shows %s, want %s still", w.files[got].Path, w.files[shown].Path)
	}
	if got := w.current; got != shown {
		t.Errorf("the fold left the file at %s, want %s", w.files[got].Path, w.files[shown].Path)
	}
	if got := countRows(w, rowHeader); got != 1 {
		t.Errorf("%d file cards, want the 1 of the file shown", got)
	}
}

// TestGroupRowTakesNoGroundColour checks that a fold leaves no mark: the row
// looks as it did before the click once the pointer is away, and the tree
// chooses nothing.
func TestGroupRowTakesNoGroundColour(t *testing.T) {
	w, tt := kindWindow(t)
	row, ok := tt.Find("Docs")
	if !ok {
		t.Fatal("no row of Docs")
	}
	probe := func() [4]uint32 {
		x, y := int(row.X+row.W-6), int(row.Y+row.H/2)
		r, g, b, a := tt.Image().RGBAAt(x, y).RGBA()
		return [4]uint32{r, g, b, a}
	}
	focused := w.treeSel
	before := probe()
	if err := tt.Click("Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// The pointer away, so that the ground of the pointer goes too.
	tt.Move(1, 1)
	tt.Frame()
	if !w.closedDirs["g:docs"] {
		t.Fatalf("the click did not fold Docs: %v", w.closedDirs)
	}
	if got := probe(); got != before {
		t.Errorf("the folded row shows %v where it showed %v: the fold marks the row", got, before)
	}
	if w.treeSel != focused {
		t.Errorf("the fold chose %q in the tree, where it chose %q", w.treeSel, focused)
	}
}

// TestGroupFoldsStayFolded checks that the surface does not open a group the
// reader closed: the tree follows the file shown without unfolding.
func TestGroupFoldsStayFolded(t *testing.T) {
	w, tt := kindWindow(t)
	if err := tt.Click("Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	w.selectTreeFile(fileIndex(t, w, "README.md"))
	tt.Frame()
	if !w.closedDirs["g:docs"] {
		t.Fatal("the file shown opened the group the reader folded")
	}
}

// TestGroupRowTakesNoChoice checks what a click on a group does not do: it
// writes no choice into the tree, it asks the list for no focus, and it
// leaves the focus on the diffs, so the keys keep working there. It runs in
// both file views, from a tree that chose nothing and from one that chose
// another file.
func TestGroupRowTakesNoChoice(t *testing.T) {
	for _, view := range []string{fileViewOne, fileViewStack} {
		for _, chosen := range []string{"", "f:README.md"} {
			w, tt := kindWindow(t)
			keepFileView(t)
			w.setFileView(view)
			w.current = fileIndex(t, w, ".github/workflows/ci.yml")
			tt.Frame()
			w.treeSel = chosen
			tt.Frame()
			tt.Frame()
			if !tt.Focused("Changes") {
				t.Fatalf("[%s] the diffs do not start with the focus", view)
			}
			shown := w.shownFile()
			if err := tt.Click("Code"); err != nil {
				t.Fatal(err)
			}
			if !w.closedDirs["g:code"] {
				t.Fatalf("[%s] the click did not fold Code: %v", view, w.closedDirs)
			}
			if got := w.shownFile(); got != shown {
				t.Errorf("[%s] the click showed %s, want %s still", view, w.files[got].Path, w.files[shown].Path)
			}
			if w.treeSel != chosen {
				t.Errorf("[%s] the click chose %q in the tree, where it chose %q", view, w.treeSel, chosen)
			}
			if w.focusList {
				t.Errorf("[%s] the click asked the list for the focus", view)
			}
			if !tt.Focused("Changes") {
				t.Errorf("[%s] the click took the focus from the diffs", view)
			}
			if tt.Focused("Changed files") {
				t.Errorf("[%s] the click moved the focus into the tree", view)
			}
			if view != fileViewOne {
				continue
			}
			// The keys still work on the diffs, where they were.
			files := w.viewFiles()
			at := slices.Index(files, w.shownFile())
			if at < 0 || at+1 >= len(files) {
				t.Fatalf("the file shown has no file after it: %v of %v", at, files)
			}
			tt.Key(0, ui.KeyBracketRight)
			tt.Frame()
			if got, want := w.shownFile(), files[at+1]; got != want {
				t.Errorf("[%s] \" ] \" showed %s after the fold, want %s", view, w.files[got].Path, w.files[want].Path)
			}
		}
	}
}

// TestGroupChipGoesToTheGroup checks the chips, which stand in for the tree
// where there is no sidebar: one file at a time, a chip shows the first file
// of its group, and a second click shows that file again rather than every
// change.
func TestGroupChipGoesToTheGroup(t *testing.T) {
	w, tt := kindWindow(t)
	keepFileView(t)
	w.setFileView(fileViewOne)
	w.sidebarShown = false
	tt.Frame()
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if path := w.files[w.current].Path; path != "app.go" {
		t.Fatalf("%s shows, want app.go, the first file of Code", path)
	}
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if path := w.files[w.current].Path; path != "app.go" {
		t.Errorf("the second click showed %s, want app.go still", path)
	}
}

// TestFileViewStepsAcrossGroups checks that the file view walks every file:
// from the last file of a group, the next one is the first file of the group
// after it.
func TestFileViewStepsAcrossGroups(t *testing.T) {
	w, tt := kindWindow(t)
	keepFileView(t)
	w.setFileView(fileViewOne)
	w.showGroup(groupIndex(t, w, "code"))
	tt.Frame()
	// Every file of the review, in the order the groups put them: the steps
	// cross from one group into the next.
	want := []string{"app.go", "app_test.go", ".github/workflows/ci.yml", "go.mod", "dist/bundle.js"}
	for i, path := range want {
		if got := w.files[w.current].Path; got != path {
			t.Fatalf("%s shows, want %s", got, path)
		}
		if i == len(want)-1 {
			break
		}
		w.stepFile(1)
	}
	if at, total := w.fileNav(); at != 6 || total != 6 {
		t.Errorf("%d / %d, want 6 / 6 at the last file", at, total)
	}
	if w.canStep(1) {
		t.Error("a file comes after the last")
	}
}

// TestGroupHeadsItsFiles checks that a group heads its files in the tree:
// the one place saying where they belong.
func TestGroupHeadsItsFiles(t *testing.T) {
	w, tt := kindWindow(t)
	rows := w.visibleTree()
	at := slices.IndexFunc(rows, func(r treeRow) bool { return r.key == "g:code" })
	if at < 0 {
		t.Fatalf("no row of Code in the tree: %q", tt.Texts())
	}
	if rows[at].depth != 0 {
		t.Errorf("the group shows at depth %d", rows[at].depth)
	}
	if at+1 >= len(rows) || rows[at+1].depth <= rows[at].depth {
		t.Errorf("the group heads no files: %v", rows)
	}
}

// TestSummaryShowsWholeInTheStack checks the agent's review with every file
// stacked: it tells of every change, and every change shows, so it is not
// folded.
func TestSummaryShowsWholeInTheStack(t *testing.T) {
	w, tt := kindWindow(t)
	if _, ok := agent.ByName("pi"); !ok {
		t.Skip("no Pi among the agents")
	}
	st := w.analysisFor(w.source)
	st.result = &agent.Analysis{
		OverallSummary: "The changes range wide.",
		Agent:          "pi",
		Model:          "pi/sonnet",
		GeneratedAt:    time.Now(),
		Groups:         []agent.Group{{Key: "all", Label: "Everything", FilePaths: []string{"app.go"}}},
	}
	w.regroup()
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	if w.summaryFolded() {
		t.Error("the stacked view folds the review of every change")
	}
	if !tt.HasText("The changes range wide.") {
		t.Fatalf("the card does not show every change: %q", tt.Texts())
	}
}

// TestFoldSnapshots saves the frames of a fold, to look at.
func TestFoldSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	w, tt := kindWindow(t)
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	tt.Frame()
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	// The pointer away, so that neither frame shows the ground of the
	// pointer: the two frames differ by the fold alone.
	tt.Move(1, 1)
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "fold-collapsed")
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Move(1, 1)
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "fold-expanded")
}

package main

import (
	"image/color"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFileTreeSelectsOnPressFromOutsideSidebar(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	w.treeSel = "f:main.go"
	draft := ""
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = c.Theme().Accent
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, "Selected "+w.treeSel)
			w.fileTree(c)
			ui.TextInput(c, &draft).Label("Editor")
		})
	}, 300, 500)
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	old, ok := tt.Find("main.go")
	if !ok {
		t.Fatal("the selected file row is missing")
	}
	r, ok := tt.Find("old.txt")
	if !ok {
		t.Fatal("the file row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "f:old.txt" || !tt.HasText("Selected f:old.txt") {
		t.Fatalf("selected %q, frame %q", w.treeSel, tt.Texts())
	}
	if !tt.Focused("Changed files") {
		t.Fatal("the file tree did not take focus on press")
	}
	pixel := func(r ui.Rect) color.RGBA {
		return tt.Image().RGBAAt(int(r.X+r.W-4), int(r.Y+r.H/2))
	}
	want := color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: 255}
	if pixel(old) == want || pixel(r) != want {
		t.Fatalf("old fill %v, new fill %v, want only the new file to have %v", pixel(old), pixel(r), want)
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "f:old.txt" {
		t.Fatal("release changed the selection again")
	}
}

func TestFileTreeDirectoryDisclosureWaitsForRelease(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	r, ok := tt.Find("docs")
	if !ok {
		t.Fatal("the directory row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "d:docs" || w.closedDirs["d:docs"] {
		t.Fatal("press should select the directory and keep it open")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if !w.closedDirs["d:docs"] {
		t.Fatal("release did not close the directory")
	}
}

func TestHistorySelectsOnPressFromOutsideSidebar(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.tab = 1
	tt.Frame()
	if err := tt.Click("Filter history"); err != nil {
		t.Fatal(err)
	}
	r, ok := tt.Find("First commit")
	if !ok {
		t.Fatal("the commit row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.source.kind != sourceCommit || w.commit == nil || w.commit.Subject != "First commit" {
		t.Fatalf("press did not select the commit: %+v", w.source)
	}
	if !tt.Focused("History") {
		t.Fatal("the history did not take focus on press")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
}

// groupByKind groups the files of a test window by their kind.
func groupByKind(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	w, tt := newTestWindow(t, testRepo(t))
	w.groupPref = groupKind
	w.regroup()
	// The surface keeps the file it showed while the rows change; the tests
	// read it from the top.
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	return w, tt
}

// TestSidebarGroupsInTheTree checks the groups of the sidebar: a row each,
// with the count, the change and the share viewed of its files, and the
// files of each under it.
func TestSidebarGroupsInTheTree(t *testing.T) {
	w, tt := groupByKind(t)
	for _, name := range []string{"Docs", "Code"} {
		if r, ok := tt.Find(name); !ok || r.X >= w.sidebarWidth {
			t.Errorf("%q is not a row of the sidebar: %v %v", name, r, ok)
		}
	}
	for _, want := range []string{"1 file · +1 −1", "0/1", "3 files · +5 −2", "0/3"} {
		if !tt.HasText(want) {
			t.Errorf("the groups miss %q: %q", want, tt.Texts())
		}
	}
	// The files of a group come under it, indented.
	rows := w.visibleTree()
	gi := groupIndex(t, w, "code")
	at := slices.IndexFunc(rows, func(r treeRow) bool { return r.key == "g:"+w.groups[gi].key })
	if at < 0 {
		t.Fatalf("no row of the group in %q", tt.Texts())
	}
	if rows[at].depth != 0 {
		t.Errorf("the group shows at depth %d, not at the top", rows[at].depth)
	}
	for _, i := range w.groupFiles(gi) {
		key := "f:" + w.files[i].Path
		j := slices.IndexFunc(rows, func(r treeRow) bool { return r.key == key })
		if j < at || rows[j].depth <= rows[at].depth {
			t.Errorf("%s shows at %d, depth %d, not under its group", w.files[i].Path, j, rows[j].depth)
		}
	}
	// A group's row folds its files away, and leaves the surface on the
	// file it showed: it is a fold, not a place to go.
	shown := w.current
	r, ok := tt.Find("Code")
	if !ok {
		t.Fatal("the Code group has no row")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !w.closedDirs["g:"+w.groups[gi].key] {
		t.Fatalf("the press did not fold Code: %v", w.closedDirs)
	}
	if w.current != shown {
		t.Errorf("the fold showed file %d, want %d still", w.current, shown)
	}
}

// TestSidebarTreeShowsEveryGroup checks that the tree keeps every file
// while the surface shows one file alone: going to a file changes the
// diffs, not the tree.
func TestSidebarTreeShowsEveryGroup(t *testing.T) {
	w, tt := groupByKind(t)
	w.showGroup(groupIndex(t, w, "code"))
	tt.Frame()
	rows := w.visibleTree()
	for _, path := range []string{"main.go", "docs/long.txt", "old.txt"} {
		if !slices.ContainsFunc(rows, func(r treeRow) bool { return r.key == "f:"+path }) {
			t.Errorf("no row of %s while the Code group shows alone", path)
		}
	}
	// The groups count every file, the one the filter hid as well.
	w.filter = "src/new.go"
	w.buildTree()
	w.rowsDirty = true
	tt.Frame()
	if len(w.groups) != 2 || !tt.HasText("3 files · +5 −2") {
		t.Fatalf("the filter reached the groups: %d groups, %q", len(w.groups), tt.Texts())
	}
	for _, r := range w.visibleTree() {
		if n := w.treeItems[r.key]; n != nil && !n.dir && n.name != "new.go" {
			t.Errorf("the tree kept %s", n.name)
		}
	}
}

// TestSidebarGroupFolds checks the room a group takes: its chevron folds
// its files away, and the group stays, with its counts.
func TestSidebarGroupFolds(t *testing.T) {
	w, tt := groupByKind(t)
	if err := tt.Click("Hide the files of Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.closedDirs["g:docs"] || w.inFiles("docs/long.txt") {
		t.Fatalf("Docs closed: %v, files %q", w.closedDirs, tt.Texts())
	}
	if !tt.HasText("1 file · +1 −1") {
		t.Errorf("the group folded its counts away: %q", tt.Texts())
	}
	if err := tt.Click("Show the files of Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.closedDirs["g:docs"] || !w.inFiles("docs/long.txt") {
		t.Fatalf("Docs did not open: %v", w.closedDirs)
	}
}

// inFiles reports whether a file has a row in the files tree.
func (w *window) inFiles(path string) bool {
	return slices.ContainsFunc(w.visibleTree(), func(r treeRow) bool { return r.key == "f:"+path })
}

// TestOnePlaceForTheGroups checks that the groups show in one place: the
// sidebar, which names each once, while the surface's chips stay away.
func TestOnePlaceForTheGroups(t *testing.T) {
	w, tt := groupByKind(t)
	count := func(s string) int {
		n := 0
		for _, text := range tt.Texts() {
			if text == s {
				n++
			}
		}
		return n
	}
	if _, ok := tt.Find("Code"); !ok {
		t.Fatal("the sidebar has no row of the groups")
	}
	if got := count("0/1"); got != 1 {
		t.Fatalf("with the sidebar: %d counts of Docs, want 1", got)
	}
	if !tt.HasText("0/1 viewed") {
		t.Error("the surface's group heading lost its count")
	}
	w.sidebarShown = false
	tt.Frame()
	// Hidden, the groups show as chips, which count as the sidebar did.
	if _, ok := tt.Find("Filter files"); ok {
		t.Error("the tree showed, hidden")
	}
	if got := count("0/1"); got != 1 {
		t.Fatalf("without the sidebar: %d counts of Docs, want 1", got)
	}
	if !tt.HasText("0/1 viewed") {
		t.Error("the surface's group heading lost its count")
	}
}

func TestGroupSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	w, tt := groupByKind(t)
	snapshot(t, tt, "groups-sidebar")
	// A narrow window, where the sidebar goes and the chips come.
	w.sidebarShown = false
	narrow := ui.NewTester(w.view, 760, 860)
	narrow.Frame()
	snapshot(t, narrow, "groups-chips")
}

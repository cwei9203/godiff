package main

import (
	"image/color"
	"os"
	"path/filepath"
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

func TestSidebarGroupsSection(t *testing.T) {
	w, tt := groupByKind(t)
	// The groups come first, each with its files' count, its change, and
	// the share of them viewed.
	for _, name := range []string{"Docs", "Code"} {
		if r, ok := tt.Find(name); !ok || r.X >= w.sidebarWidth {
			t.Errorf("%q is not a row of the sidebar: %v %v", name, r, ok)
		}
	}
	for _, want := range []string{"1 file · +1 −1", "0/1", "3 files · +5 −2", "0/3"} {
		if !tt.HasText(want) {
			t.Errorf("the groups section misses %q", want)
		}
	}
	// A group's row takes the surface to the group.
	r, ok := tt.Find("Code")
	if !ok {
		t.Fatal("the Code group has no row")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if w.current < 0 || w.files[w.current].group != 1 {
		t.Fatalf("the surface stayed on file %d, group %v", w.current, w.files[w.current].group)
	}
}

func TestSidebarGroupsIgnoreTheFilter(t *testing.T) {
	w, tt := groupByKind(t)
	w.filter = "src/new.go"
	w.buildTree()
	w.rowsDirty = true
	tt.Frame()
	// The groups count every file, the one the filter hid as well.
	if len(w.groups) != 2 || !tt.HasText("3 files · +5 −2") {
		t.Fatalf("the filter reached the groups: %d groups, %q", len(w.groups), tt.Texts())
	}
	// The files show only those the filter keeps.
	for _, r := range w.visibleFiles() {
		if n := w.treeItems[r.key]; n != nil && !n.dir && n.name != "new.go" {
			t.Errorf("the files section kept %s", n.name)
		}
	}
}

func TestSidebarSectionsCollapse(t *testing.T) {
	w, tt := groupByKind(t)
	if err := tt.Click("Hide the files of Docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.closedDirs["g:docs"] || w.inFiles("docs/long.txt") {
		t.Fatalf("Docs closed: %v, files %q", w.closedDirs, tt.Texts())
	}
	if err := tt.Click("Files"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Changed files"); ok {
		t.Fatalf("the Files section shows closed: %q", tt.Texts())
	}
	if _, ok := tt.Find("Groups"); !ok {
		t.Fatal("closing the files closed the groups too")
	}
	if groups, _ := state.sections(); !groups {
		t.Error("the store closed the Groups section too")
	}
	if _, files := state.sections(); files {
		t.Error("the store did not keep the Files section closed")
	}
}

// inFiles reports whether a file has a row in the files section.
func (w *window) inFiles(path string) bool {
	return slices.ContainsFunc(w.visibleFiles(), func(r treeRow) bool { return r.key == "f:"+path })
}

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
	// The sidebar shows the groups: the surface shows them once more, as
	// the headings of its blocks, and the chips, which say the same, stay
	// away.
	if _, ok := tt.Find("Groups"); !ok {
		t.Fatal("the sidebar has no Groups section")
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
	if _, ok := tt.Find("Groups"); ok {
		t.Error("the sidebar showed, hidden")
	}
	if got := count("0/1"); got != 1 {
		t.Fatalf("without the sidebar: %d counts of Docs, want 1", got)
	}
	if !tt.HasText("0/1 viewed") {
		t.Error("the surface's group heading lost its count")
	}
}

func TestSidebarSectionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &store{}
	s.openAt(filepath.Join(dir, "state.json"))
	s.setSections(false, true)
	again := &store{}
	again.openAt(filepath.Join(dir, "state.json"))
	if groups, files := again.sections(); groups || !files {
		t.Fatalf("kept groups %v, files %v", groups, files)
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

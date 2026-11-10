package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// asideRepo makes a repository whose files group into four kinds, so that
// the blocks of the groups show one after the other.
func asideRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	writeFile(t, dir, "docs/readme.md", "Old docs.\n")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "util.go", "package main\n\nfunc util() {}\n")
	writeFile(t, dir, "main_test.go", "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n")
	writeFile(t, dir, "package.json", "{\"name\": \"x\", \"version\": \"1.0.0\"}\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")

	writeFile(t, dir, "docs/readme.md", "New docs, with more to say.\n")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"hi\") }\n")
	writeFile(t, dir, "util.go", "package main\n\nfunc util() { println(1) }\n")
	writeFile(t, dir, "main_test.go", "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) { t.Log(1) }\n")
	writeFile(t, dir, "package.json", "{\"name\": \"x\", \"version\": \"1.0.1\"}\n")
	return dir
}

// wideWindow opens the review of a repository grouped by kind, in a window
// wide enough to show the aside, settling the width it is measured at.
func wideWindow(t *testing.T, dir string) (*window, *ui.Tester) {
	t.Helper()
	w, tt := newTestWindow(t, dir)
	tt.SetSize(1600, 1000)
	// The sidebar is the tree the test would otherwise find a file in
	// first; the aside is what the width is about.
	w.sidebarShown = false
	w.setGrouping(groupKind)
	tt.Frame()
	tt.Frame()
	return w, tt
}

func TestGroupAside(t *testing.T) {
	w, tt := wideWindow(t, asideRepo(t))
	if !w.asideOn() {
		t.Fatalf("no aside in %v wide", w.columnW)
	}
	if len(w.groups) != 4 {
		t.Fatalf("groups: %d", len(w.groups))
	}
	gi := w.topGroup()
	if gi != 0 {
		t.Fatalf("the aside lists group %d, not the first", gi)
	}
	// The aside lists every file of the group it heads.
	for _, i := range w.groupFiles(gi) {
		f := w.files[i]
		r, ok := tt.Find(f.Path)
		if !ok {
			t.Errorf("%s is not in the aside", f.Path)
			continue
		}
		if r.X > w.asideWidth {
			t.Errorf("%s shows at %v, past the aside", f.Path, r.X)
		}
	}
	if _, ok := tt.Find("Files in " + w.groups[gi].label); !ok {
		t.Errorf("no list of the files of %s in %q", w.groups[gi].label, tt.Texts())
	}
	// The block of a group heads itself with its strip.
	if _, ok := tt.Find("Group " + w.groups[gi].label); !ok {
		t.Errorf("no strip heading %s in %q", w.groups[gi].label, tt.Texts())
	}
	snapshot(t, tt, "aside")

	// The aside heads the block of the second group once it shows.
	// The surface is short here, so that a block scrolls to the top.
	tt.SetSize(1600, 560)
	tt.Frame()
	tt.Frame()
	w.revealGroup(1)
	tt.Frame()
	tt.Frame()
	if gi := w.topGroup(); gi != 1 {
		t.Fatalf("the aside lists group %d, not the second", gi)
	}
	if _, ok := tt.Find("Files in " + w.groups[1].label); !ok {
		t.Errorf("no list of the files of %s in %q", w.groups[1].label, tt.Texts())
	}

	// Clicking a file of the aside shows it in the diffs.
	files := w.groupFiles(1)
	last := files[len(files)-1]
	if len(files) < 2 {
		t.Fatalf("group of %d files", len(files))
	}
	if err := tt.Click(w.files[last].Path); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.current != last {
		t.Errorf("showing %s, not %s", w.files[w.current].Path, w.files[last].Path)
	}
	snapshot(t, tt, "aside-groups")
}

func TestGroupAsideHiddenWhenNarrow(t *testing.T) {
	w, tt := wideWindow(t, asideRepo(t))
	if !w.asideOn() {
		t.Fatalf("no aside in %v wide", w.columnW)
	}
	// A window narrower than the aside asks for, its sidebar shown, reviews
	// in one column, as it did before the aside.
	w.sidebarShown = true
	tt.SetSize(1280, 860)
	tt.Frame()
	tt.Frame()
	if w.asideOn() {
		t.Fatalf("aside in %v wide", w.columnW)
	}
	if tt.HasText("Files in ") {
		t.Errorf("the aside shows in one column: %q", tt.Texts())
	}
	for _, want := range []string{"Docs", "Collapse Docs"} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	snapshot(t, tt, "aside-narrow")

	// Wider again, it comes back.
	tt.SetSize(1600, 1000)
	tt.Frame()
	tt.Frame()
	if !w.asideOn() {
		t.Fatal("the aside did not come back")
	}
}

// TestAsideDrag checks that the divisor beside the aside widens it, within
// its bounds, and keeps the width it left.
func TestAsideDrag(t *testing.T) {
	defer func() { state.data.AsideWidth = 0 }()
	w, tt := wideWindow(t, asideRepo(t))
	if w.asideWidth != asideDefault {
		t.Fatalf("aside of %v", w.asideWidth)
	}
	// The divisor runs down the aside's right edge.
	x, y := 12+w.asideWidth+0.5, float32(400)
	tt.Press(x, y)
	tt.Frame()
	tt.Move(x+18, y)
	tt.Frame()
	if w.asideWidth != asideDefault+18 {
		t.Fatalf("aside of %v while dragging", w.asideWidth)
	}
	tt.Release(x+18, y)
	tt.Frame()
	tt.Frame()
	for i := 0; i < 100 && state.asideWidth() != asideDefault+18; i++ {
		tt.Frame()
		time.Sleep(time.Millisecond)
	}
	if got := state.asideWidth(); got != asideDefault+18 {
		t.Errorf("kept %v", got)
	}

	// A drag past the bounds stops at them.
	tt.Press(12+w.asideWidth+0.5, y)
	tt.Frame()
	tt.Move(12+w.asideWidth+200, y)
	tt.Frame()
	if w.asideWidth != asideMax {
		t.Errorf("aside of %v, dragged wide", w.asideWidth)
	}
}

// TestGroupStripPinned checks that the strip heading a group stays at the
// top of the surface while the group's diffs scroll under it, and that the
// aside lists that same group.
func TestGroupStripPinned(t *testing.T) {
	w, tt := wideWindow(t, asideRepo(t))
	// A surface shorter than the changes, so that scrolling moves them.
	tt.SetSize(1600, 560)
	tt.Frame()
	tt.Frame()
	w.buildRows()
	// Into the lines of the second group's second file, past its header.
	at := -1
	for i := range w.rows {
		if w.rows[i].kind == rowGroup && int(w.rows[i].gap) == 1 {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("no block of the second group")
	}
	headers := 0
	target := at
	for i := at; i < len(w.rows); i++ {
		if w.rows[i].kind != rowHeader {
			continue
		}
		if headers++; headers == 2 {
			target = min(i+2, len(w.rows)-1)
			break
		}
	}
	w.list.ScrollTo(target, ui.Start)
	tt.Frame()
	tt.Frame()
	if gi := w.topGroup(); gi != 1 {
		t.Fatalf("the surface shows group %d, not the second", gi)
	}
	if _, ok := tt.Find("Files in " + w.groups[1].label); !ok {
		t.Errorf("the aside lists no files of %s: %q", w.groups[1].label, tt.Texts())
	}
	r1, ok := tt.Find("Group " + w.groups[1].label)
	if !ok {
		t.Fatalf("no strip of %s atop the surface: %q", w.groups[1].label, tt.Texts())
	}
	// The surface scrolled, and the strip did not: the header it rides is
	// pinned there.
	_, before := w.list.Visible()
	w.list.ScrollTo(target+2, ui.Start)
	tt.Frame()
	tt.Frame()
	if _, after := w.list.Visible(); after < before {
		t.Fatalf("the surface did not scroll: rows to %d, then to %d", before, after)
	}
	r2, ok := tt.Find("Group " + w.groups[1].label)
	if !ok {
		t.Fatalf("no strip of %s after scrolling: %q", w.groups[1].label, tt.Texts())
	}
	if d := r2.Y - r1.Y; d < -1 || d > 1 {
		t.Errorf("the strip of the group moved from %v to %v", r1.Y, r2.Y)
	}
	if gi := w.topGroup(); gi != 1 {
		t.Errorf("the surface shows group %d after scrolling, the aside too", gi)
	}
	// The strip rides the file's header, which the list pins: the strip is
	// not a row of its own.
	pinned := -1
	for i := target; i >= 0; i-- {
		if w.rows[i].kind == rowHeader {
			pinned = i
			break
		}
	}
	if pinned < 0 || !w.list.Header(pinned) {
		t.Errorf("the header of the file read is not the row the list pins: %d", pinned)
	}
	snapshot(t, tt, "aside-pinned")
}

// TestGroupBlocks checks that the surface shows one block per group, in the
// order of the groups, and that a block heads its own files.
func TestGroupBlocks(t *testing.T) {
	w, tt := wideWindow(t, asideRepo(t))
	tt.Frame()
	w.buildRows()
	var blocks []int
	for i := range w.rows {
		if w.rows[i].kind == rowGroup {
			blocks = append(blocks, int(w.rows[i].gap))
		}
	}
	if len(blocks) != len(w.groups) {
		t.Fatalf("%d blocks of %d groups", len(blocks), len(w.groups))
	}
	for i, gi := range blocks {
		if gi != i {
			t.Errorf("block %d of group %d", i, gi)
		}
	}
	// The files of a block are those of its group, after its block.
	in := -1
	for i := range w.rows {
		r := &w.rows[i]
		if r.kind == rowGroup {
			in = int(r.gap)
			continue
		}
		if r.kind != rowHeader {
			continue
		}
		if got := w.files[r.file].group; got != in {
			t.Errorf("%s in block %d, of group %d", w.files[r.file].Path, in, got)
		}
	}
}

// TestAsideWidthRoundTrip checks that the width of the aside is kept and
// read back as the sidebar's layout is, in the same store.
func TestAsideWidthRoundTrip(t *testing.T) {
	state.data.AsideWidth = 254
	defer func() { state.data.AsideWidth = 0 }()
	w, _ := newTestWindow(t, asideRepo(t))
	if w.asideWidth != 254 {
		t.Errorf("the window took %v", w.asideWidth)
	}

	state.setAsideWidth(232)
	data, err := json.Marshal(state.data)
	if err != nil {
		t.Fatal(err)
	}
	var back storeData
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if got := (&store{data: back}).asideWidth(); got != 232 {
		t.Errorf("read back %v", got)
	}
	if !strings.Contains(string(data), `"asideWidth":232`) {
		t.Errorf("no asideWidth in %s", data)
	}

	// A width out of the bounds the aside takes is kept within them.
	state.setAsideWidth(1000)
	if got := state.asideWidth(); got != asideMax {
		t.Errorf("wide width %v", got)
	}
	state.setAsideWidth(10)
	if got := state.asideWidth(); got != asideMin {
		t.Errorf("small width %v", got)
	}
	state.data.AsideWidth = 0
	if got := state.asideWidth(); got != asideDefault {
		t.Errorf("default %v", got)
	}
}

// TestAsideVirtualized checks that a group of many files still builds only
// the rows in view, in the diffs and in the aside.
func TestAsideVirtualized(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	const files = 120
	for i := range files {
		writeFile(t, dir, fmt.Sprintf("src/f%03d.go", i), "package src\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")
	for i := range files {
		writeFile(t, dir, fmt.Sprintf("src/f%03d.go", i), fmt.Sprintf("package src\n\n// F%d is %d.\nconst F%d = %d\n", i, i, i, i))
	}
	w, _ := wideWindow(t, dir)
	if !w.asideOn() {
		t.Fatalf("no aside in %v wide", w.columnW)
	}
	if len(w.rows) < files {
		t.Fatalf("%d rows of %d files", len(w.rows), files)
	}
	first, last := w.list.Visible()
	if last-first > 40 {
		t.Errorf("%d rows of the diffs in view", last-first)
	}
	gi := w.topGroup()
	afirst, alast := w.asideList.Visible()
	if alast-afirst >= len(w.groupFiles(gi)) {
		t.Errorf("the aside shows %d of its %d files", alast-afirst, len(w.groupFiles(gi)))
	}
}

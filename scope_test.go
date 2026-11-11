package main

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// scopeRepo makes a repository whose changes fall into six groups: docs,
// code, tests, config, deps, and generated. Five scopes of files tell apart
// what shows, where a repository of one kind of file would not.
func scopeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	files := map[string]string{
		"README.md":                "# scope\n",
		"app.go":                   "package app\n\nfunc Run() {}\n",
		"app_test.go":              "package app\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n",
		".github/workflows/ci.yml": "name: ci\non: push\n",
		"go.mod":                   "module scope\n\ngo 1.24\n",
		"dist/bundle.js":           "// built\n",
	}
	for path, content := range files {
		writeFile(t, dir, path, content)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")
	changed := map[string]string{
		"README.md":                "# scope\n\nThe all changes row, then one group.\n",
		"app.go":                   "package app\n\nfunc Run() { println(\"run\") }\n",
		"app_test.go":              "package app\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) { t.Log(1) }\n",
		".github/workflows/ci.yml": "name: ci\non: [push, pull_request]\n",
		"go.mod":                   "module scope\n\ngo 1.24\n\nrequire example.com/dep v1.2.3\n",
		"dist/bundle.js":           "// built\nconsole.log(1);\n",
	}
	for path, content := range changed {
		writeFile(t, dir, path, content)
	}
	return dir
}

// scopeWindow opens the review of scopeRepo grouped by kind, with every
// change shown.
func scopeWindow(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	w, tt := newTestWindow(t, scopeRepo(t))
	w.groupPref = groupKind
	w.regroup()
	tt.Frame()
	if len(w.groups) < 5 {
		t.Fatalf("%d groups, want the five the scopes filter", len(w.groups))
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

// TestScopeAllShowsEveryFile is the scope a window opens on: every change,
// every group's strip, every file's card.
func TestScopeAllShowsEveryFile(t *testing.T) {
	w, _ := scopeWindow(t)
	if !w.scope.all() {
		t.Fatalf("a window opens on %+v", w.scope)
	}
	w.buildRows()
	if got := countRows(w, rowHeader); got != len(w.files) {
		t.Errorf("%d file cards, want %d", got, len(w.files))
	}
	if got := countRows(w, rowGroup); got != len(w.groups) {
		t.Errorf("%d group strips, want %d", got, len(w.groups))
	}
}

// TestScopeGroupShowsItsFiles checks that a group shown alone shows its own
// files, its strip once, and nothing of the other groups.
func TestScopeGroupShowsItsFiles(t *testing.T) {
	w, tt := scopeWindow(t)
	gi := groupIndex(t, w, "code")
	w.showGroup(gi)
	tt.Frame()
	if w.scope.kind != scopeOneGroup || w.scope.group != gi {
		t.Fatalf("scope %+v, want the group %q", w.scope, w.groups[gi].key)
	}
	files := w.groupFiles(gi)
	w.buildRows()
	if got := countRows(w, rowHeader); got != len(files) {
		t.Errorf("%d file cards, want the %d files of %s", got, len(files), w.groups[gi].label)
	}
	if got := countRows(w, rowGroup); got != 1 {
		t.Errorf("%d group strips, want 1", got)
	}
	for i := range w.rows {
		r := &w.rows[i]
		if r.kind != rowHeader {
			continue
		}
		if w.files[r.file].group != gi {
			t.Errorf("the card of %s shows, of another group", w.files[r.file].Path)
		}
	}
	// The tree keeps every file: the scope decides the diffs, not the way
	// to them, so the other groups stay a click away.
	for _, other := range []string{"README.md", "go.mod", "app.go"} {
		if !w.inFiles(other) {
			t.Errorf("the tree lost %s while the group %s shows alone", other, w.groups[gi].label)
		}
	}
}

// TestScopeFileShowsOneCard checks that a file shown alone shows its card,
// under a strip naming its group, and no other card.
func TestScopeFileShowsOneCard(t *testing.T) {
	w, tt := scopeWindow(t)
	i := fileIndex(t, w, "app.go")
	w.showFile(i)
	tt.Frame()
	if w.scope.kind != scopeOneFile || w.scope.file != i {
		t.Fatalf("scope %+v, want the file app.go", w.scope)
	}
	w.buildRows()
	if got := countRows(w, rowHeader); got != 1 {
		t.Errorf("%d file cards, want 1", got)
	}
	if got := countRows(w, rowEnd); got != 1 {
		t.Errorf("%d card ends, want 1", got)
	}
	if got := countRows(w, rowGroup); got != 1 {
		t.Errorf("%d group strips, want the 1 of the file's group", got)
	}
	if w.files[w.current].Path != "app.go" {
		t.Errorf("the file at the top is %s", w.files[w.current].Path)
	}
	// The strip names the group the file belongs to, and the card says it
	// too, in its breadcrumb.
	if _, ok := tt.Find("Files in " + w.groups[w.files[i].group].label); ok {
		t.Error("the aside shows in a window too narrow for it")
	}
	if _, ok := tt.Find("Group " + w.groups[w.files[i].group].label); !ok {
		t.Errorf("no breadcrumb of the group in %q", tt.Texts())
	}
}

// TestScopeClicksToggle checks the way back: the group row and the file row
// show their part alone, and a second click on the same row goes back to
// every change.
func TestScopeClicksToggle(t *testing.T) {
	w, tt := scopeWindow(t)
	// A group of the sidebar.
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.scope.kind != scopeOneGroup {
		t.Fatalf("the click showed %+v", w.scope)
	}
	if err := tt.Click("Code"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.scope.all() {
		t.Fatalf("the second click showed %+v, want every change", w.scope)
	}
	// A file of the files section.
	if err := tt.Click("app.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.scope.kind != scopeOneFile || w.files[w.scope.file].Path != "app.go" {
		t.Fatalf("the click showed %+v, want app.go", w.scope)
	}
	if err := tt.Click("app.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.scope.all() {
		t.Fatalf("the second click showed %+v, want every change", w.scope)
	}
}

// TestScopeAllChangesRow checks the row heading the groups: it goes back to
// every change from a narrower scope.
func TestScopeAllChangesRow(t *testing.T) {
	w, tt := scopeWindow(t)
	w.showGroup(groupIndex(t, w, "deps"))
	tt.Frame()
	if err := tt.Click("All changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.scope.all() {
		t.Fatalf("the row showed %+v, want every change", w.scope)
	}
}

// TestScopeShortcutShowsAll checks ⌘⇧A, which comes back from any scope.
func TestScopeShortcutShowsAll(t *testing.T) {
	w, tt := scopeWindow(t)
	w.showFile(fileIndex(t, w, "app.go"))
	tt.Frame()
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
	tt.Frame()
	if !w.scope.all() {
		t.Fatalf("⌘⇧A showed %+v, want every change", w.scope)
	}
}

// TestScopeMarksTheGroup checks that the group shown has its row in the
// tree, and its files under it: the one place saying where the reader is.
func TestScopeMarksTheGroup(t *testing.T) {
	w, tt := scopeWindow(t)
	gi := groupIndex(t, w, "code")
	w.showGroup(gi)
	tt.Frame()
	rows := w.visibleTree()
	at := slices.IndexFunc(rows, func(r treeRow) bool { return r.key == "g:"+w.groups[gi].key })
	if at < 0 {
		t.Fatalf("no row of %s in the tree: %q", w.groups[gi].label, tt.Texts())
	}
	if rows[at].depth != 0 {
		t.Errorf("the group shows at depth %d", rows[at].depth)
	}
	if at+1 >= len(rows) || rows[at+1].depth <= rows[at].depth {
		t.Errorf("the group heads no files: %v", rows)
	}
}

// TestSummaryFoldsInANarrowScope checks the agent's review in a scope
// narrower than every change: one line, which opens the card again.
func TestSummaryFoldsInANarrowScope(t *testing.T) {
	w, tt := scopeWindow(t)
	speeds, ok := agent.ByName("pi")
	if !ok {
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
	_ = speeds
	w.regroup()
	// The card comes above the changes, whose place the list keeps.
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	if !tt.HasText("The changes range wide.") {
		t.Fatalf("the card does not show every change: %q", tt.Texts())
	}
	w.showGroup(groupIndex(t, w, "code"))
	tt.Frame()
	line, ok := tt.Find("AI Review · Pi · pi/sonnet · just now")
	if !ok {
		t.Fatalf("no folded line of the review in %q", tt.Texts())
	}
	if tt.HasText("The changes range wide.") {
		t.Error("the card shows whole in a scope narrower than every change")
	}
	// The line opens the card again.
	tt.ClickAt(line.X+line.W/2, line.Y+line.H/2)
	tt.Frame()
	if !w.summaryOpen || !tt.HasText("The changes range wide.") {
		t.Fatalf("the line did not open the card: open %v, %q", w.summaryOpen, tt.Texts())
	}
}

// TestScopeSnapshots saves the three frames of the scopes, to look at.
func TestScopeSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	w, tt := scopeWindow(t)
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "scope-all")
	w.showGroup(groupIndex(t, w, "code"))
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "scope-group")
	w.showFile(fileIndex(t, w, "app.go"))
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, "scope-file")
}

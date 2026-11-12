package main

import (
	"slices"
	"testing"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// viewOf marks a file of the review viewed, by its path, as a click on its
// card does.
func viewOf(t *testing.T, w *window, path string) {
	t.Helper()
	i := slices.IndexFunc(w.files, func(f *fileState) bool { return f.Path == path })
	if i < 0 {
		t.Fatalf("%s is not among the files", path)
	}
	w.setViewed(w.files[i], true)
}

// at is the path of the file the surface shows alone.
func at(w *window) string {
	if shown := w.shownFile(); shown >= 0 {
		return w.files[shown].Path
	}
	return ""
}

// TestNextUnviewedStepsFromTheFileShown checks the step to the file not
// viewed yet, both ways, round the review: the files after the one shown
// first, then the ones before it.
func TestNextUnviewedStepsFromTheFileShown(t *testing.T) {
	w, _ := fileViewWindow(t)
	// The third file of the eight, with the two after it viewed.
	w.current = 2
	viewOf(t, w, "main.go")
	viewOf(t, w, "other.go")
	if !w.nextUnviewed(1) {
		t.Fatal("no file to step to, of the six not viewed")
	}
	if got := at(w); got != "util.go" {
		t.Errorf("stepped on to %q, want the first file not viewed after the third", got)
	}
	// Back the other way: the two viewed are passed over.
	if !w.nextUnviewed(-1) {
		t.Fatal("no file to step back to")
	}
	if got := at(w); got != "docs/c.md" {
		t.Errorf("stepped back to %q, want the third file, none of the two viewed", got)
	}
	// Round: from the last of the eight, the step goes on to the first.
	for _, path := range []string{"main.go", "other.go", "util.go", "main_test.go", "util_test.go"} {
		viewOf(t, w, path)
	}
	w.current = 7
	if !w.nextUnviewed(1) {
		t.Fatal("no file to step to, round the review")
	}
	if got := at(w); got != "docs/a.md" {
		t.Errorf("stepped on to %q from the last file, want the first", got)
	}
}

// TestNextUnviewedWithEveryFileViewed checks the review every file of is
// viewed: the surface stays where it is, and says so.
func TestNextUnviewedWithEveryFileViewed(t *testing.T) {
	w, tt := fileViewWindow(t)
	tt.Frame()
	for _, f := range w.files {
		viewOf(t, w, f.Path)
	}
	w.current = 3
	tt.Frame()
	if w.nextUnviewed(1) {
		t.Error("the step found a file not viewed, of a review every file of is viewed")
	}
	if got := at(w); got != "main.go" {
		t.Errorf("the surface moved to %q, want it left where it was", got)
	}
	// The bar above the file says why nothing moved.
	w.stepUnviewed(1)
	tt.Frame()
	if !tt.HasText(textAllViewed()) {
		t.Errorf("%q is not in the bar: %q", textAllViewed(), tt.Texts())
	}
	if got := at(w); got != "main.go" {
		t.Errorf("the button moved the surface to %q", got)
	}
	// A file viewed again is one to step to, and the bar stops saying so.
	w.setViewed(w.files[0], false)
	tt.Frame()
	if tt.HasText(textAllViewed()) {
		t.Errorf("the bar still says every file is viewed: %q", tt.Texts())
	}
}

// TestNextUnviewedKeysAndButton checks the keys and the button of the bar:
// n steps on, ⇧n back, and the button steps on as n does.
func TestNextUnviewedKeysAndButton(t *testing.T) {
	w, tt := fileViewWindow(t)
	tt.Frame()
	viewOf(t, w, "docs/a.md")
	viewOf(t, w, "docs/b.md")
	w.current = 0
	tt.Frame()
	tt.Key(0, ui.KeyN)
	tt.Frame()
	if got := at(w); got != "docs/c.md" {
		t.Fatalf("n stepped to %q, want the third file", got)
	}
	// Back, past the two viewed, and round to the last.
	tt.Key(ui.Shift, ui.KeyN)
	tt.Frame()
	if got := at(w); got != "util_test.go" {
		t.Errorf("⇧n stepped back to %q, want the last file, round the two viewed", got)
	}
	// The button does the same, round from the last, past the two viewed.
	if err := tt.Click("Next unviewed file"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := at(w); got != "docs/c.md" {
		t.Errorf("the button stepped to %q, want the third file, round the two viewed", got)
	}
}

// TestStackKeepsNForTheNotes checks the other view: with every file stacked,
// n is still the notes', and the files are not stepped through.
func TestStackKeepsNForTheNotes(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	// A review of one file, with a note on its second line.
	w.analyses[w.source] = &analysisState{result: &agent.Analysis{
		Agent: "claude", Groups: []agent.Group{{
			Key: "code", Label: "Code", FilePaths: []string{"main.go"},
			LineNotes: []agent.LineNote{{Path: "main.go", Side: "additions", Line: 2, Text: "This is the change."}},
		}},
	}}
	w.groupPref = groupNone
	w.regroup()
	tt.Frame()
	if len(w.notes["main.go"]) == 0 {
		t.Fatalf("no note of the review, of %d notes", len(w.orderedNotes()))
	}
	tt.Key(0, ui.KeyN)
	tt.Frame()
	if w.noteSel == nil {
		t.Fatalf("n found no note with every file stacked: %q", tt.Texts())
	}
	if w.noteSel.path != "main.go" || w.noteSel.line != 2 {
		t.Errorf("n went to the note of %s:%d", w.noteSel.path, w.noteSel.line)
	}
}

// TestViewedKeyMarksTheFileShown checks the one key of a file's action: v
// marks the file the surface shows viewed, which folds it, and v again
// unmarks it, as the card's checkbox does.
func TestViewedKeyMarksTheFileShown(t *testing.T) {
	w, tt := fileViewWindow(t)
	// One file at a time, the surface shows that one: the key acts on it.
	shown := w.shownFile()
	if shown < 0 {
		t.Fatalf("no file shown of %d", len(w.files))
	}
	f := w.files[shown]
	if w.isViewed(f) {
		t.Fatalf("%s is viewed before the key", f.Path)
	}
	tt.Key(0, ui.KeyV)
	tt.Frame()
	if !w.isViewed(f) {
		t.Fatalf("v did not mark %s viewed", f.Path)
	}
	if !f.collapsed {
		t.Errorf("%s is viewed and not folded", f.Path)
	}
	tt.Key(0, ui.KeyV)
	tt.Frame()
	if w.isViewed(f) {
		t.Fatalf("v did not unmark %s", f.Path)
	}
	if f.collapsed {
		t.Errorf("%s is unmarked and still folded", f.Path)
	}
	// A field with the focus keeps its letters: v is a letter to type.
	tt.Click(msg().FilterFiles())
	tt.Frame()
	tt.Type("v")
	tt.Frame()
	if w.isViewed(f) {
		t.Fatalf("v in the filter marked %s viewed", f.Path)
	}
}

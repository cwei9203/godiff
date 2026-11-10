package main

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/review"
)

// addText adds a comment on a line and writes its text.
func addText(w *window, path string, sd side, start, line int, text string) {
	w.addComment(path, sd, start, line)
	w.comments[len(w.comments)-1].text = text
}

// reviewTask is the task file the export wrote, for a window on a repository
// with three comments: two with text, one still an empty draft.
func reviewTask(t *testing.T) (*window, string) {
	t.Helper()
	w, _ := newTestWindow(t, testRepo(t))
	w.groupPref = groupKind
	w.regroup()
	addText(w, "main.go", sideNew, 3, 3, "Name it for what it returns.")
	addText(w, "docs/long.txt", sideOld, 5, 6, "This range goes away.")
	w.addComment("src/new.go", sideNew, 2, 2) // an empty draft: no comment yet
	w.exportReviewTask()
	data, err := os.ReadFile(review.Path(w.repo.Root))
	if err != nil {
		t.Fatalf("no task file: %v", err)
	}
	return w, string(data)
}

func TestExportReviewTask(t *testing.T) {
	w, text := reviewTask(t)
	// The change is named, the repository is named, and the task asks for
	// the fixes.
	for _, want := range []string{
		"# Review: Uncommitted changes",
		"local working-tree diff of `" + w.repo.Root + "`",
		"Fix each comment in place.",
		"**2 unresolved comments.**",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
	// The comments sit under the group of their file, anchored on their
	// line, an old range marked old.
	docs := between(text, "## Docs", "## Code")
	if want := "- `docs/long.txt` (modified) (around line 5)\n  - `docs/long.txt:5-6 (old)`: This range goes away."; !strings.Contains(docs, want) {
		t.Errorf("no %q in the docs group\n%s", want, docs)
	}
	code := between(text, "## Code", "**2 unresolved")
	if want := "  - `main.go:3`: Name it for what it returns."; !strings.Contains(code, want) {
		t.Errorf("no %q in the code group\n%s", want, code)
	}
	// The draft is no work to ask for, and the file it is on shows nowhere.
	if strings.Contains(text, "src/new.go") {
		t.Errorf("the empty draft is in the task\n%s", text)
	}
	// The export and a repair's prompt are one text: the same document,
	// rendered once.
	if want := review.Render(w.reviewDocument()); text != want {
		t.Errorf("the export differs from the document\n%s\n%s", text, want)
	}
}

func TestReviewDocumentStatus(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	doc := w.reviewDocument()
	status := map[string]string{}
	for _, f := range doc.Other {
		status[f.Path] = f.Status
	}
	// A file git calls untracked is added, and a deleted one deleted; a
	// file without a group is no group's.
	for path, want := range map[string]string{"src/new.go": "added", "old.txt": "deleted", "main.go": "modified"} {
		if status[path] != want {
			t.Errorf("%s: %q, not %q", path, status[path], want)
		}
	}
	if len(doc.Groups) != 0 {
		t.Errorf("ungrouped files came in groups: %d", len(doc.Groups))
	}
}

func TestExportNeedsComments(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	if cmd, ok := findCommand(w, "Export Review Task"); ok && cmd.run != nil {
		t.Error("the export is offered with no comment to export")
	}
	addText(w, "main.go", sideNew, 3, 3, "Name it.")
	if cmd, ok := findCommand(w, "Export Review Task"); !ok || cmd.run == nil {
		t.Error("the export is not offered with a comment to export")
	}
}

// between is the part of a text from one line to the next.
func between(text, from, to string) string {
	i := strings.Index(text, from)
	if i < 0 {
		return ""
	}
	rest := text[i+len(from):]
	if j := strings.Index(rest, to); j >= 0 {
		return rest[:j]
	}
	return rest
}

// findCommand finds a command of the bar by its title.
func findCommand(w *window, title string) (command, bool) {
	for _, cmd := range w.commands() {
		if cmd.title == title {
			return cmd, true
		}
	}
	return command{}, false
}

package main

import (
	"os"
	"slices"
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

// scopedReview is a window on three comments in two groups: two of the code
// group, one of the docs group.
func scopedReview(t *testing.T) *window {
	t.Helper()
	w, _ := newTestWindow(t, testRepo(t))
	w.groupPref = groupKind
	w.regroup()
	addText(w, "main.go", sideNew, 3, 3, "Name it for what it returns.")
	addText(w, "docs/long.txt", sideOld, 5, 6, "This range goes away.")
	addText(w, "src/new.go", sideNew, 2, 2, "Drop the comment.")
	w.addComment("main.go", sideNew, 9, 9) // an empty draft: no comment yet
	return w
}

// showFile puts a file at the top of the surface, which a scope's file and
// group are read from.
func showFile(t *testing.T, w *window, path string) {
	t.Helper()
	for i, f := range w.files {
		if f.Path == path {
			w.current = i
			return
		}
	}
	t.Fatalf("no %s in the review", path)
}

// documentPaths is the comments and files a document asks about.
func documentPaths(doc review.Document) (comments, files []string) {
	for _, c := range doc.Comments {
		comments = append(comments, c.Path)
	}
	for _, g := range doc.Groups {
		for _, f := range g.Files {
			files = append(files, f.Path)
		}
	}
	for _, f := range doc.Other {
		files = append(files, f.Path)
	}
	return comments, files
}

// TestReviewDocumentScopes filters the one assembler: all of the review, the
// group of the file in view, or that file alone, and only the comments still
// unresolved.
func TestReviewDocumentScopes(t *testing.T) {
	w := scopedReview(t)
	showFile(t, w, "docs/long.txt")

	comments, _ := documentPaths(w.reviewDocumentFor(fixScopeAll))
	if len(comments) != 3 {
		t.Fatalf("all of it asks about %v, want three comments", comments)
	}

	// The group of the file in view: the docs, and only the docs.
	doc := w.reviewDocumentFor(fixScopeGroup)
	comments, files := documentPaths(doc)
	if len(comments) != 1 || comments[0] != "docs/long.txt" {
		t.Errorf("the docs group asks about %v", comments)
	}
	if len(files) != 1 || files[0] != "docs/long.txt" {
		t.Errorf("the docs group holds %v", files)
	}
	if len(doc.Groups) != 1 || doc.Groups[0].Label != "Docs" {
		t.Errorf("the groups are %+v", doc.Groups)
	}
	if text := review.Render(doc); strings.Contains(text, "main.go") {
		t.Errorf("another group's file is in the task\n%s", text)
	}

	// The file in view alone.
	showFile(t, w, "main.go")
	doc = w.reviewDocumentFor(fixScopeFile)
	comments, files = documentPaths(doc)
	if len(comments) != 1 || comments[0] != "main.go" {
		t.Errorf("the file asks about %v", comments)
	}
	if len(files) != 1 || files[0] != "main.go" {
		t.Errorf("the file's group holds %v", files)
	}
	if text := review.Render(doc); !strings.Contains(text, "**1 unresolved comment.**") {
		t.Errorf("the task does not count one comment\n%s", text)
	}

	// The code group holds the three code files: main.go, src/new.go and the
	// deleted old.txt.
	showFile(t, w, "src/new.go")
	comments, files = documentPaths(w.reviewDocumentFor(fixScopeGroup))
	if len(comments) != 2 {
		t.Errorf("the code group asks about %v", comments)
	}
	if len(files) != 3 || !slices.Contains(files, "main.go") || !slices.Contains(files, "src/new.go") {
		t.Errorf("the code group holds %v", files)
	}

	// All of the review is no filter at all: it is the export's document.
	showFile(t, w, "main.go")
	if w.scopePaths(fixScopeAll) != nil {
		t.Error("all of the review came as a filter")
	}
	if got, want := review.Render(w.reviewDocumentFor(fixScopeAll)), review.Render(w.reviewDocument()); got != want {
		t.Errorf("all of the review differs from the export\n%s\n%s", got, want)
	}
}

// TestFixCommand needs the comments to fix and an agent that can fix them,
// and keeps the export beside it for a machine with no agent.
func TestFixCommand(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	w.settings.FixAgent = "claude"
	if cmd, ok := findCommand(w, "Fix comments with an agent"); !ok || cmd.run != nil {
		t.Error("the repair is offered with no comment to fix")
	}
	addText(w, "main.go", sideNew, 3, 3, "Name it for what it returns.")
	cmd, ok := findCommand(w, "Fix comments with an agent")
	if !ok || cmd.run == nil {
		t.Fatal("the repair is not offered with a comment to fix")
	}
	if want := "1 comment · Claude Code"; cmd.hint != want {
		t.Errorf("the hint is %q, want %q", cmd.hint, want)
	}
	// The agent that reviews is the one that fixes when the settings name
	// none for repairs; with no agent at all, nothing is offered.
	w.settings.FixAgent, w.settings.AIAgent = "", "codex"
	if got := w.fixHint(); got != "1 comment · Codex" {
		t.Errorf("without an agent of its own, the repair names %q", got)
	}
	w.settings.AIAgent = ""
	if cmd, ok := findCommand(w, "Fix comments with an agent"); !ok || cmd.run != nil {
		t.Error("the repair is offered with no agent installed")
	}
	if cmd, ok := findCommand(w, "Export review task"); !ok || cmd.run == nil {
		t.Error("the export went with the repair")
	}
}

func TestExportNeedsComments(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	if cmd, ok := findCommand(w, "Export review task"); ok && cmd.run != nil {
		t.Error("the export is offered with no comment to export")
	}
	addText(w, "main.go", sideNew, 3, 3, "Name it.")
	if cmd, ok := findCommand(w, "Export review task"); !ok || cmd.run == nil {
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

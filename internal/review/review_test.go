package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doc() Document {
	return Document{
		Title: "Converge the cache reads",
		Repo:  "/repo/godiff",
		Groups: []Group{
			{Label: "Cache layer", Summary: "One read path, behaviour unchanged.",
				Files: []File{{Path: "internal/cache/store.go", Status: "modified"}, {Path: "internal/cache/lru.go", Status: "modified"}}},
			{Label: "Imports", Summary: "",
				Files: []File{{Path: "internal/import/api.go", Status: "added"}}},
			{Label: "Untouched", Files: []File{{Path: "docs/readme.md", Status: "modified"}}},
		},
		Other: []File{{Path: "go.mod", Status: "modified"}},
		Comments: []Comment{
			{Path: "internal/cache/store.go", Line: 57, Text: "Nil is not handled; the caller panics."},
			{Path: "internal/cache/store.go", Line: 12, End: 14, Text: "This range goes away.", Side: SideOld},
			{Path: "internal/import/api.go", Line: 3, Text: "Name the error type.", Resolved: true},
			{Path: "go.mod", Line: 8, Text: "Drop the unused dependency."},
			{Path: "internal/cache/lru.go", Line: 4, Text: "  "},
		},
	}
}

func TestRender(t *testing.T) {
	text := Render(doc())
	// The header, the task and the count of what is left to do.
	for _, want := range []string{
		"# Review: Converge the cache reads  (local diff, 5 files)",
		"local working-tree diff of `/repo/godiff`",
		"Fix each comment in place. Keep changes minimal",
		"**3 unresolved comments.** Fix each one, then summarize what you changed and why.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
	// A comment sits under the group of its file, with its line anchor, and
	// the comments of a file read in order.
	cache := between(t, text, "## Cache layer", "## Other changed files")
	for _, want := range []string{
		"One read path, behaviour unchanged.",
		"- `internal/cache/store.go` (modified) (around line 12)",
		"  - `internal/cache/store.go:12-14 (old)`: This range goes away.",
		"  - `internal/cache/store.go:57`: Nil is not handled; the caller panics.",
	} {
		if !strings.Contains(cache, want) {
			t.Errorf("no %q in the cache group\n%s", want, cache)
		}
	}
	// A resolved comment is no work: neither its file nor its group shows,
	// and it is not counted.
	if strings.Contains(text, "internal/import/api.go") || strings.Contains(text, "## Imports") || strings.Contains(text, "Name the error type") {
		t.Errorf("a resolved comment is rendered\n%s", text)
	}
	// A file no group holds goes last, with the comments on it.
	if other := text[strings.Index(text, "## Other changed files"):]; !strings.Contains(other, "`go.mod:8`: Drop the unused dependency.") {
		t.Errorf("other files\n%s", other)
	}
	// The group whose files have no unresolved comment shows not at all, nor
	// a file whose only comment is blank.
	if strings.Contains(text, "docs/readme.md") || strings.Contains(text, "## Untouched") || strings.Contains(text, "lru.go") {
		t.Errorf("a group with nothing to do is rendered\n%s", text)
	}
}

func TestRenderNothingToDo(t *testing.T) {
	if got := Render(Document{Title: "x"}); got != "" {
		t.Errorf("a review without comments renders %q", got)
	}
	d := doc()
	for i := range d.Comments {
		d.Comments[i].Resolved = true
	}
	if got := Render(d); got != "" {
		t.Errorf("a review of resolved comments renders %q", got)
	}
}

func TestRenderUngroupedComment(t *testing.T) {
	d := Document{Title: "x", Comments: []Comment{{Path: "README.md", Line: 2, Text: "Say why."}}}
	text := Render(d)
	if !strings.Contains(text, "## Other changed files") || !strings.Contains(text, "- `README.md` (around line 2)") {
		t.Errorf("a comment on a file the review does not list:\n%s", text)
	}
	if !strings.Contains(text, "**1 unresolved comment.**") {
		t.Errorf("one comment, counted as one:\n%s", text)
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	if path != filepath.Join(dir, ".review", "REVIEW.md") {
		t.Fatalf("path %q", path)
	}
	if err := Write(path, doc()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != Render(doc()) {
		t.Error("the file is not what the renderer renders")
	}
	// Nothing to ask writes nothing, not an empty task.
	empty := filepath.Join(dir, "empty", "REVIEW.md")
	if err := Write(empty, Document{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(empty); err == nil {
		t.Error("a review without comments wrote a file")
	}
}

// between is the text between two headings.
func between(t *testing.T, text, from, to string) string {
	t.Helper()
	i := strings.Index(text, from)
	j := strings.Index(text, to)
	if i < 0 || j < i {
		t.Fatalf("no %q before %q in\n%s", from, to, text)
	}
	return text[i:j]
}

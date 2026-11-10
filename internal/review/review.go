// Package review renders a review's comments as the task file an agent
// reads: .review/REVIEW.md, as pulls.review has it. The app writes it for an
// agent that fixes the change in place, and sends the same text to one
// directly, so the text is rendered to a string first and written after.
package review

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Side is the side of the change a comment is on.
type Side uint8

const (
	// SideNew is the file as the change leaves it.
	SideNew Side = iota
	// SideOld is the file as the change found it.
	SideOld
)

// Comment is a review comment on a line, or a range of lines, of a file.
type Comment struct {
	Path string
	// Line is the line the comment is on: of the new file, or of the old one
	// for SideOld.
	Line int
	// End is the last line of a range, 0 for one line.
	End      int
	Side     Side
	Text     string
	Resolved bool
}

// File is a changed file, as the review shows it.
type File struct {
	Path   string
	Status string // modified, added, renamed… as it reads, "" for nothing
}

// Group is a group of the review: files serving one intent, with the comments
// made on them.
type Group struct {
	Label   string
	Summary string
	Files   []File
}

// Document is a review to render.
type Document struct {
	// Title is what the change is, as the review names it.
	Title string
	// Repo is the repository's path, for the task's first line.
	Repo string
	// Groups are the groups the files came in, in the order they show.
	Groups []Group
	// Other are the files no group holds.
	Other []File
	// Comments are every comment of the review, which Render places under
	// the group of their file.
	Comments []Comment
}

// Path is where a repository's review task file goes: inside the repository,
// so that an agent run in it reads the file whatever its working directory.
func Path(root string) string {
	return filepath.Join(root, ".review", "REVIEW.md")
}

// Write renders the document at path, making the directories it needs, and
// writes nothing when there is no unresolved comment: a task file left from
// a review already acted on would ask again for what was done.
func Write(path string, doc Document) error {
	return WriteText(path, Render(doc))
}

// WriteText writes a task file's text at path, making the directories it
// needs, and writes nothing when there is no text. A repair writes the same
// text it is about to send, so it is rendered once and handed to both.
func WriteText(path, text string) error {
	if text == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// Render renders the document as the Markdown an agent reads: the groups in
// the order they show, the comments of a file under its group, and the files
// no group holds under a heading of their own. Only unresolved comments are
// rendered, and a document without one renders to "".
func Render(doc Document) string {
	var comments []Comment
	for _, c := range doc.Comments {
		if !c.Resolved && strings.TrimSpace(c.Text) != "" && c.Path != "" {
			comments = append(comments, c)
		}
	}
	if len(comments) == 0 {
		return ""
	}
	byPath := map[string][]Comment{}
	for _, c := range comments {
		byPath[c.Path] = append(byPath[c.Path], c)
	}
	for p := range byPath {
		slices.SortStableFunc(byPath[p], func(a, b Comment) int {
			if a.Line != b.Line {
				return a.Line - b.Line
			}
			return int(a.Side) - int(b.Side)
		})
	}
	// The change's files, in the order the review lists them, and the status
	// a file shows; a file only a comment names is still part of the change.
	var files []string
	status := map[string]string{}
	add := func(list []File) {
		for _, f := range list {
			if _, ok := status[f.Path]; ok {
				continue
			}
			status[f.Path] = f.Status
			files = append(files, f.Path)
		}
	}
	for _, g := range doc.Groups {
		add(g.Files)
	}
	add(doc.Other)
	other := slices.Clone(doc.Other)
	for _, c := range comments {
		if _, ok := status[c.Path]; !ok {
			status[c.Path] = ""
			files = append(files, c.Path)
			other = append(other, File{Path: c.Path})
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Review: %s  (local diff, %s)\n\n", doc.Title, plural(len(files), "file"))
	if doc.Repo != "" {
		fmt.Fprintf(&b, "You are acting on code-review comments left on a local working-tree diff of `%s`.\n", doc.Repo)
	}
	b.WriteString("Fix each comment in place. Keep changes minimal: do not refactor beyond what a comment asks.\n")
	for _, g := range doc.Groups {
		var section strings.Builder
		for _, f := range g.Files {
			writeFile(&section, f, byPath[f.Path])
		}
		if section.Len() == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", cmp.Or(g.Label, "Changes"))
		if s := strings.TrimSpace(g.Summary); s != "" {
			b.WriteString(s + "\n\n")
		}
		b.WriteString(section.String())
	}
	var section strings.Builder
	for _, f := range other {
		writeFile(&section, File{Path: f.Path, Status: status[f.Path]}, byPath[f.Path])
	}
	if section.Len() > 0 {
		b.WriteString("\n## Other changed files\n\n" + section.String())
	}
	fmt.Fprintf(&b, "\n**%d unresolved %s.** Fix each one, then summarize what you changed and why.\n", len(comments), word(len(comments), "comment"))
	return b.String()
}

// writeFile writes a file and the comments on it, nothing for a file without
// one: a file the task does not name needs no work.
func writeFile(b *strings.Builder, f File, comments []Comment) {
	if len(comments) == 0 {
		return
	}
	fmt.Fprintf(b, "- `%s`%s (around line %d)\n", f.Path, statusSuffix(f.Status), comments[0].Line)
	for _, c := range comments {
		fmt.Fprintf(b, "  - `%s`: %s\n", anchor(c), indent(strings.TrimSpace(c.Text), "\n    "))
	}
}

// anchor is where a comment is: path:line, or path:first-last for a range,
// marked old for a line the change removes.
func anchor(c Comment) string {
	where := fmt.Sprintf("%s:%d", c.Path, c.Line)
	if c.End > c.Line {
		where = fmt.Sprintf("%s:%d-%d", c.Path, c.Line, c.End)
	}
	if c.Side == SideOld {
		where += " (old)"
	}
	return where
}

func statusSuffix(status string) string {
	if status == "" {
		return ""
	}
	return " (" + status + ")"
}

// indent indents every line but the first of a text.
func indent(s, prefix string) string {
	return strings.ReplaceAll(s, "\n", prefix)
}

// plural is a count and what it counts: 1 file, 2 files.
func plural(n int, what string) string {
	return fmt.Sprintf("%d %s", n, word(n, what))
}

// word is a word in the singular, or in the plural when it counts several.
func word(n int, what string) string {
	if n == 1 {
		return what
	}
	return what + "s"
}

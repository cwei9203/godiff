package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/review"
	"github.com/egoist/mygo"
)

// fixScope is how much of the review a repair is asked about: all of it, the
// group of the file in view, or that file alone.
type fixScope uint8

const (
	fixScopeAll fixScope = iota
	fixScopeGroup
	fixScopeFile
)

// fixScopeLabels names the scopes, in the order they show.
var fixScopeLabels = []string{"All Comments", "This Group", "This File"}

// fixScopeOrder is the scopes as fixScopeLabels names them.
var fixScopeOrder = []fixScope{fixScopeAll, fixScopeGroup, fixScopeFile}

// reviewDocument assembles the review's task file from the window: the
// comments it holds, under the groups of the files they are on, as pulls.review
// has it. It is the one assembler: A-4 writes what it builds, and A-5 sends the
// same text to an agent.
func (w *window) reviewDocument() review.Document {
	return w.reviewDocumentFor(fixScopeAll)
}

// reviewDocumentFor assembles the review's task file of one scope: the same
// document reviewDocument builds, of the comments of the files in scope, under
// the groups those files are in. The filter is applied to what the assembler
// builds, so that there is one format and not two.
func (w *window) reviewDocumentFor(scope fixScope) review.Document {
	doc := review.Document{Title: w.reviewTitle()}
	if w.repo != nil {
		doc.Repo = w.repo.Root
	}
	paths := w.scopePaths(scope)
	inScope := func(path string) bool { return paths == nil || paths[path] }
	for _, c := range w.comments {
		// An empty draft is no comment yet, and a comment already fixed is
		// no work to ask for.
		if !c.pending() || !inScope(c.path) {
			continue
		}
		first, last := c.line, c.line
		if c.start != 0 {
			first, last = min(c.start, c.line), max(c.start, c.line)
		}
		sd := review.SideNew
		if c.side == sideOld {
			sd = review.SideOld
		}
		doc.Comments = append(doc.Comments, review.Comment{Path: c.path, Line: first, End: last, Side: sd, Text: c.text})
	}
	for gi, g := range w.groups {
		group := review.Group{Label: g.label, Summary: g.summary}
		for _, i := range w.groupFiles(gi) {
			if !inScope(w.files[i].Path) {
				continue
			}
			group.Files = append(group.Files, review.File{Path: w.files[i].Path, Status: statusWord(w.files[i])})
		}
		// A group with none of the scope's files in it says nothing of them.
		if len(group.Files) > 0 {
			doc.Groups = append(doc.Groups, group)
		}
	}
	for _, f := range w.files {
		if f.group < 0 && inScope(f.Path) {
			doc.Other = append(doc.Other, review.File{Path: f.Path, Status: statusWord(f)})
		}
	}
	return doc
}

// scopePaths is the paths of the files a scope covers: the file in view, the
// group it is in, or nil for every path of the review.
func (w *window) scopePaths(scope fixScope) map[string]bool {
	if scope == fixScopeAll || w.current < 0 || w.current >= len(w.files) {
		return nil
	}
	if scope == fixScopeFile {
		return map[string]bool{w.files[w.current].Path: true}
	}
	paths := map[string]bool{}
	for _, i := range w.groupFiles(w.files[w.current].group) {
		if i >= 0 && i < len(w.files) {
			paths[w.files[i].Path] = true
		}
	}
	return paths
}

// reviewTitle names the change as the review does, in the words the agent's
// review is given: the task file's first line says what it is about.
func (w *window) reviewTitle() string {
	switch w.source.kind {
	case sourceWorkingTree:
		title := "Uncommitted changes"
		if w.branch != "" {
			title += " on " + w.branch
		}
		return title
	case sourceBranch:
		return fmt.Sprintf("The changes of %s since it branched from %s", w.branch, w.source.ref)
	case sourceCommit:
		if w.commit != nil && w.commit.Subject != "" {
			return w.commit.Subject
		}
		return "Commit " + shortHash(w.source.ref)
	case sourcePull:
		if w.pr != nil && w.pr.meta != nil && w.pr.meta.Title != "" {
			return w.pr.meta.Title
		}
		_, n := w.source.pull()
		return fmt.Sprintf("Pull request #%d", n)
	}
	return ""
}

// statusWord is how a file changed, as the task file reads it: modified,
// added, deleted.
func statusWord(f *fileState) string {
	if f.Status == diff.Untracked {
		// git calls a new file untracked before it is added; a reviewer
		// reads it as added.
		return "added"
	}
	return strings.ToLower(diff.Status(f.Status).Label())
}

// exportReviewTask writes the review's task file in the repository, for an
// agent to read, and shows that it did the way copying the comments shows it.
func (w *window) exportReviewTask() {
	if w.repo == nil {
		return
	}
	path := review.Path(w.repo.Root)
	if err := review.Write(path, w.reviewDocument()); err != nil {
		mygo.Dialog.Error("Could not write the review task file", err.Error())
		return
	}
	w.copiedAt = w.now
	// The mark of what was written lasts two seconds, as the copy's does:
	// a frame is drawn after them to clear it.
	if w.win != nil {
		w.background(func() {
			time.Sleep(2 * time.Second)
			w.update(func() {})
		})
	}
}

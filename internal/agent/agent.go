// Package agent asks a coding agent installed on the machine (Claude Code,
// Codex, OpenCode or Pi) to review a change: to group its files by intent,
// summarize it, and note what deserves a reviewer's attention, as
// pulls.review does with a local agent.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/shellenv"
)

// Agent is a coding agent's command line.
type Agent struct {
	Name  string // the command: claude, codex, opencode or pi
	Label string
}

// All are the agents the app knows, in the order it prefers them.
var All = []Agent{
	{Name: "claude", Label: "Claude Code"},
	{Name: "codex", Label: "Codex"},
	{Name: "opencode", Label: "OpenCode"},
	{Name: "pi", Label: "Pi"},
}

// ByName returns the agent of a command.
func ByName(name string) (Agent, bool) {
	for _, a := range All {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Installed returns the agents found in the user's PATH.
func Installed() []Agent {
	var out []Agent
	for _, a := range All {
		if _, err := shellenv.LookPath(a.Name); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// SchemaVersion is the version of the analysis contract: an analysis of
// another version is dropped rather than read as this one.
const SchemaVersion = 1

// Analysis is an agent's review of a change.
type Analysis struct {
	// SchemaVersion is the version of the contract the analysis was made
	// under, set as it is read, not by the agent.
	SchemaVersion  int     `json:"schemaVersion"`
	OverallSummary string  `json:"overallSummary"`
	Groups         []Group `json:"groups"`
	// Locale is the language the review is written in, as BCP 47, so that
	// a review in another language is another review.
	Locale string `json:"locale,omitempty"`
	// HeadSHA is the revision of the change's new side the review covers:
	// a new commit since is a review that has gone stale.
	HeadSHA string `json:"headSha,omitempty"`
	// What produced it, and when.
	Agent       string    `json:"agent,omitempty"`
	Model       string    `json:"model,omitempty"`
	GeneratedAt time.Time `json:"generatedAt"`
}

// Category is the part of the system a group's change touches: one axis
// only, what it changes rather than why, so that a reviewer reading a list
// of groups can tell them apart at a glance.
type Category string

// The 16 categories a group has one of.
const (
	CategoryUI       Category = "ui"
	CategoryAPI      Category = "api"
	CategoryCore     Category = "core"
	CategoryData     Category = "data"
	CategoryCLI      Category = "cli"
	CategorySecurity Category = "security"
	CategoryTests    Category = "tests"
	CategoryDocs     Category = "docs"
	CategoryExamples Category = "examples"
	CategoryDeps     Category = "deps"
	CategoryBuild    Category = "build"
	CategoryScripts  Category = "scripts"
	CategoryConfig   Category = "config"
	CategoryI18n     Category = "i18n"
	CategoryAssets   Category = "assets"
	CategoryOther    Category = "other"
)

// Categories are every category, in the order a surface would list them.
var Categories = []Category{
	CategoryUI, CategoryAPI, CategoryCore, CategoryData,
	CategoryCLI, CategorySecurity, CategoryTests, CategoryDocs,
	CategoryExamples, CategoryDeps, CategoryBuild, CategoryScripts,
	CategoryConfig, CategoryI18n, CategoryAssets, CategoryOther,
}

// Valid reports whether a category is one of the 16: an agent may answer
// another, and an analysis from before categories may have none.
func (c Category) Valid() bool { return slices.Contains(Categories, c) }

// Group is a set of files serving one intent.
type Group struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Summary   string     `json:"summary"`
	Category  Category   `json:"category"`
	Critical  bool       `json:"critical"`
	FilePaths []string   `json:"filePaths"`
	FileNotes []FileNote `json:"fileNotes"`
	LineNotes []LineNote `json:"lineNotes"`
	// Missing are the paths the group claims that the change no longer
	// holds: files that left it since the analysis was made. They are kept
	// rather than dropped, so a reviewer sees what went, and a group whose
	// paths all went is kept too.
	Missing []string `json:"missing,omitempty"`
}

// Normalize brings an analysis to the contract it is read under: its schema
// version, and a category for every group. What is missing falls back rather
// than fail — the rest of the review is still worth showing.
func (a *Analysis) Normalize() {
	a.SchemaVersion = SchemaVersion
	for i := range a.Groups {
		if !a.Groups[i].Category.Valid() {
			a.Groups[i].Category = CategoryOther
		}
	}
}

// FileNote explains a file.
type FileNote struct {
	Path     string `json:"path"`
	Text     string `json:"text"`
	Critical bool   `json:"critical"`
}

// LineNote explains a line: of the new file for the side "additions", of
// the old one for "deletions".
type LineNote struct {
	Path     string `json:"path"`
	Side     string `json:"side"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
	Critical bool   `json:"critical"`
}

// Critical counts the groups and notes marked critical.
func (a *Analysis) Critical() int {
	n := 0
	for _, g := range a.Groups {
		if g.Critical {
			n++
		}
		for _, f := range g.FileNotes {
			if f.Critical {
				n++
			}
		}
		for _, l := range g.LineNotes {
			if l.Critical {
				n++
			}
		}
	}
	return n
}

// File is a changed file, as the prompt shows it.
type File struct {
	Path, OldPath        string
	Status               byte // git's letter: A, M, D, R
	Additions, Deletions int
	Binary, Generated    bool
	// Patch is the file's hunks as a unified diff, "" when not shown.
	Patch string
}

// Request is a change to review.
type Request struct {
	Title       string
	Description string
	URL         string
	Commits     []string // the subjects, oldest first
	Files       []File
	// Dir is where the agent runs: the repository.
	Dir string
	// Checkout tells the agent whether the files in Dir are those of the
	// change: false when the change is of another revision.
	Checkout bool
	// Rev is the revision of the change's new side, for git show.
	Rev string
	// Model is the model the agent runs with, "" for its own default.
	Model string
	// Locale is the language the review is written in, as BCP 47, "" for
	// the agent's own: a review in another language is another review.
	Locale string
}

// PromptVersion is the version of the prompt below, and of the schema it
// quotes: any change to their wording, fields or manifest that can change
// what an agent answers — including the order of the fields — must raise it,
// or an analysis made with the old prompt is read as if it were this one.
const PromptVersion = "v1"

// schema is the analysis as a JSON Schema strict enough for structured
// output: every property required, none other allowed.
const schema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["overallSummary", "groups"],
  "properties": {
    "overallSummary": {"type": "string", "description": "The intention of the change (why over what) for a reviewer who has not read it yet, in 2-4 sentences of Markdown."},
    "groups": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["key", "label", "summary", "category", "critical", "filePaths", "fileNotes", "lineNotes"],
        "properties": {
          "key": {"type": "string", "description": "Short, stable kebab-case id."},
          "label": {"type": "string", "description": "Display name, at most 4 words."},
          "summary": {"type": "string", "description": "The intention of this group, in 1-3 sentences of Markdown."},
          "category": {"type": "string", "enum": ["ui", "api", "core", "data", "cli", "security", "tests", "docs", "examples", "deps", "build", "scripts", "config", "i18n", "assets", "other"], "description": "The part of the system this group touches, not the kind of change. ui: components, screens, styles; api: endpoints, request and response shapes; core: domain logic and algorithms; data: models, storage, queries, migrations; cli: commands, flags, output; security: auth, permissions, secrets, crypto; tests: test code and fixtures; docs: documentation; examples: samples and demos; deps: dependencies and lockfiles; build: build system, bundlers, CI; scripts: the developer's or the maintenance scripts; config: runtime configuration and environment; i18n: translations and locales; assets: images, fonts, media; other: none of these."},
          "critical": {"type": "boolean", "description": "True only when the whole group deserves extra care: security, data loss, hard to revert."},
          "filePaths": {"type": "array", "items": {"type": "string"}, "description": "Paths of the manifest in this group. Every path goes into exactly one group."},
          "fileNotes": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["path", "text", "critical"],
              "properties": {
                "path": {"type": "string"},
                "text": {"type": "string", "description": "1-2 sentences a reviewer would otherwise have to work out."},
                "critical": {"type": "boolean"}
              }
            }
          },
          "lineNotes": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["path", "side", "line", "text", "critical"],
              "properties": {
                "path": {"type": "string"},
                "side": {"type": "string", "enum": ["additions", "deletions"]},
                "line": {"type": "integer"},
                "text": {"type": "string", "description": "1-2 sentences: non-obvious logic, a subtle behavior change, a likely bug, a risk."},
                "critical": {"type": "boolean"}
              }
            }
          }
        }
      }
    }
  }
}`

// inlineLimit is the size of the diffs the prompt holds; larger ones are
// written to a file the agent reads.
const inlineLimit = 200_000

// systemPrompt is the agent's instructions. fenced asks for the answer in
// a fenced block, from agents that do not take a schema.
func systemPrompt(req *Request, patchPath string, fenced bool) string {
	var files string
	switch {
	case req.Checkout:
		files = "The repository is checked out in the current directory: open its files when the hunks are not enough to tell what a change is for."
	case req.Rev != "":
		files = fmt.Sprintf("The current directory holds the repository, but checked out at another revision than this change, so its files may differ. Trust the diffs; where you can run commands, read a file as of the change with `git show %s:<path>`.", req.Rev)
	default:
		files = "Trust the diffs: the files of the current directory may differ from the change."
	}
	if patchPath != "" {
		files = fmt.Sprintf("The diffs are too large for this message: they are in %s. Read the parts you need. ", patchPath) + files
	}
	answer := "Finish by answering with the JSON object matching the schema, and nothing else."
	if fenced {
		answer = "Finish by answering with the JSON object matching the schema in one fenced ```json block, and nothing else."
	}
	language := ""
	if req.Locale != "" {
		language = fmt.Sprintf("\n<language>\nWrite every sentence of the answer in %s, keeping code, paths and identifiers as they are.\n</language>\n", req.Locale)
	}
	return `<role>
You review a code change for a colleague. You organize its changed files into review groups, so they can read the change intent by intent instead of file by file, and you point out what deserves their attention.
</role>

<grouping_principles>
- Group by intent, not by directory. One feature touching several modules is ONE group.
- Use the fewest groups that still separate independent intents. A typical change has 1-6 groups. A single-file group needs a reason.
- Tests, stories and fixtures belong to the feature they cover. Only tests unrelated to any feature form a group of their own.
- Order groups by review priority: the core change first, supporting changes next, mechanical changes (lockfiles, generated files, formatting) last.
- Every path of the manifest goes into exactly one group.
- Commit messages, when listed, hint at the author's intents. Group by the final change, not by commit.
</grouping_principles>

<output>
- "overallSummary" explains why over what. Each group's "summary" explains its intention.
- "category" is the part of the system a group touches, not why it changed: pick the one category most of its files belong to, and "other" only when none fits.
- "fileNotes" and "lineNotes" are sparing: add a note only where it saves the reviewer time (non-obvious logic, a subtle behavior change, a likely bug, a risk), never to explain the obvious. Most files need none: leave the arrays empty.
- A line note's "side" is "additions" for a line of the new file (added or unchanged), numbered as in the new file, and "deletions" for a removed line, numbered as in the old file, as the hunk headers count them.
- "critical" is true only for what deserves extra care: security, data loss, hard to revert, easy to get wrong. Most groups and notes are not critical.
- Keep code, paths and identifiers as they are.
</output>
` + language + `
<workflow>
1. Read the manifest and the diffs, and form the groups.
2. ` + files + `
3. Never modify anything: do not edit files, run builds or tests, or commit.
4. ` + answer + `
</workflow>

<schema>
` + schema + `
</schema>`
}

// userPrompt describes the change: its title, commits, files and diffs.
func userPrompt(req *Request, inline bool) string {
	var b strings.Builder
	if req.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", req.Title)
	}
	if req.URL != "" {
		fmt.Fprintf(&b, "Link: %s\n", req.URL)
	}
	if d := strings.TrimSpace(req.Description); d != "" {
		fmt.Fprintf(&b, "\n---DESCRIPTION---\n%s\n", d)
	}
	if len(req.Commits) > 1 {
		fmt.Fprintf(&b, "\n---COMMITS--- (%d, oldest first)\n%s\n", len(req.Commits), strings.Join(req.Commits, "\n"))
	}
	adds, dels := 0, 0
	for _, f := range req.Files {
		adds += f.Additions
		dels += f.Deletions
	}
	fmt.Fprintf(&b, "\n---MANIFEST--- (%d files, +%d/-%d)\n%s\n", len(req.Files), adds, dels, manifest(req.Files))
	if inline {
		fmt.Fprintf(&b, "\n---DIFFS---\n%s\n", diffs(req.Files))
	}
	return b.String()
}

// manifest lists the files by directory, with their status and counts.
func manifest(files []File) string {
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	var b strings.Builder
	dir := "\x00"
	for _, f := range sorted {
		d, name := "", f.Path
		if i := strings.LastIndexByte(f.Path, '/'); i >= 0 {
			d, name = f.Path[:i+1], f.Path[i+1:]
		}
		if d != dir {
			dir = d
			if d != "" {
				b.WriteString(d + "\n")
			}
		}
		if d != "" {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "%s  %c  +%d/-%d", name, f.Status, f.Additions, f.Deletions)
		if f.OldPath != "" && f.OldPath != f.Path {
			fmt.Fprintf(&b, "  (from %s)", f.OldPath)
		}
		switch {
		case f.Generated:
			b.WriteString("  [generated]")
		case f.Binary:
			b.WriteString("  [binary]")
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// diffs renders the files' hunks, leaving out those of generated and binary
// files, which a reviewer does not read line by line.
func diffs(files []File) string {
	var parts []string
	for _, f := range files {
		header := fmt.Sprintf("### %s", f.Path)
		if f.OldPath != "" && f.OldPath != f.Path {
			header += " (renamed from " + f.OldPath + ")"
		}
		header += fmt.Sprintf(" [%c, +%d/-%d]", f.Status, f.Additions, f.Deletions)
		switch {
		case f.Binary:
			parts = append(parts, header+"\n(binary file, no diff shown)")
		case f.Generated:
			parts = append(parts, header+"\n(generated file, diff omitted)")
		case f.Patch == "":
			parts = append(parts, header+"\n(no lines to show)")
		default:
			parts = append(parts, header+"\n"+strings.TrimRight(f.Patch, "\n"))
		}
	}
	return strings.Join(parts, "\n\n")
}

// parse reads an agent's answer, the JSON object alone or fenced, and fits
// it to the files: paths it invented go, and those it left out gather in a
// group of their own.
func parse(answer string, files []File) (*Analysis, error) {
	text := extractJSON(answer)
	if text == "" {
		return nil, errors.New("the agent's answer holds no analysis")
	}
	var a Analysis
	if err := json.Unmarshal([]byte(text), &a); err != nil {
		return nil, fmt.Errorf("the agent's answer is not an analysis: %w", err)
	}
	return fit(&a, files)
}

// fit keeps every file in one group exactly, and the notes on files of the
// change.
//
// A path a group lists that the change does not hold is kept in the group's
// Missing instead of dropped: an analysis is read after the change may have
// moved on — a new commit, a revert, a file renamed back — and a file that
// left the change is something the reviewer has to see, not something to
// lose quietly. A group whose paths all left is kept too, marked, rather
// than skipped.
//
// A path that only a note names is a different thing: no group of the answer
// claims it as one of the change's files, so nothing it says is anchored on
// the change. That is the agent inventing a file, and it goes.
func fit(a *Analysis, files []File) (*Analysis, error) {
	known := map[string]bool{}
	// renamed maps the name a file had before it was renamed to its name
	// now: an analysis older than the rename lists the old one.
	renamed := map[string]string{}
	for _, f := range files {
		known[f.Path] = true
		if f.OldPath != "" && f.OldPath != f.Path {
			renamed[f.OldPath] = f.Path
		}
	}
	// clean finds a file of the change by its path, also as an absolute
	// one, as OpenCode's explore agent gives them, and by an old name.
	clean := func(p string) string {
		p = strings.TrimPrefix(strings.TrimSpace(p), "./")
		if known[p] || !strings.HasPrefix(p, "/") {
			if to, ok := renamed[p]; ok {
				return to
			}
			return p
		}
		found := p
		for k := range known {
			if strings.HasSuffix(p, "/"+k) && (found == p || len(k) > len(found)) {
				found = k
			}
		}
		if !known[found] {
			if to, ok := renamed[found]; ok {
				return to
			}
		}
		return found
	}
	placed := map[string]bool{}
	var groups []Group
	for _, g := range a.Groups {
		var paths, missing []string
		for _, p := range g.FilePaths {
			p = clean(p)
			switch {
			case p == "" || placed[p]:
			case known[p]:
				placed[p] = true
				paths = append(paths, p)
			case !slices.Contains(missing, p):
				missing = append(missing, p)
			}
		}
		for i := range g.FileNotes {
			g.FileNotes[i].Path = clean(g.FileNotes[i].Path)
		}
		for i := range g.LineNotes {
			g.LineNotes[i].Path = clean(g.LineNotes[i].Path)
		}
		// A note stays on a file the group holds, and on one that left it:
		// what it says may be why the file went. Notes on a file the answer
		// otherwise never mentions go with it.
		anchored := func(p string) bool { return known[p] || slices.Contains(missing, p) }
		g.FileNotes = slices.DeleteFunc(g.FileNotes, func(n FileNote) bool { return !anchored(n.Path) || strings.TrimSpace(n.Text) == "" })
		g.LineNotes = slices.DeleteFunc(g.LineNotes, func(n LineNote) bool {
			return !anchored(n.Path) || n.Line <= 0 || strings.TrimSpace(n.Text) == "" || (n.Side != "additions" && n.Side != "deletions")
		})
		if len(paths) == 0 && len(missing) == 0 {
			continue
		}
		g.FilePaths = paths
		g.Missing = missing
		if g.Key == "" {
			g.Key = fmt.Sprintf("group-%d", len(groups)+1)
		}
		if strings.TrimSpace(g.Label) == "" {
			g.Label = "Changes"
		}
		groups = append(groups, g)
	}
	if len(groups) == 0 {
		return nil, errors.New("the agent grouped none of the files")
	}
	var rest []string
	for _, f := range files {
		if !placed[f.Path] {
			rest = append(rest, f.Path)
		}
	}
	if len(rest) > 0 {
		groups = append(groups, Group{Key: "other-changes", Label: "Other changes", Category: CategoryOther, Summary: "Files the analysis left out.", FilePaths: rest})
	}
	a.Groups = groups
	a.Normalize()
	return a, nil
}

// extractJSON returns the last fenced JSON block of a text, else what lies
// between its first { and its last }.
func extractJSON(s string) string {
	if i := strings.LastIndex(s, "```json"); i >= 0 {
		rest := s[i+len("```json"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
	}
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return ""
}

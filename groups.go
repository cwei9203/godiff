package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// groupMode is how the review groups its files, as pulls.review does: by
// their kind, or by intent as an agent's review has them.
type groupMode uint8

const (
	groupAuto groupMode = iota // by the agent's review once there is one, else by kind for pull requests
	groupNone
	groupKind
	groupAI
)

// scopeKind is what the surface shows: every change, the files of one
// group, or one file.
type scopeKind uint8

const (
	// scopeAll shows the review as a whole.
	scopeAll scopeKind = iota
	// scopeOneGroup shows a group's files, under its strip.
	scopeOneGroup
	// scopeOneFile shows one file's card, under its group's strip.
	scopeOneFile
)

// scope is the part of the review the surface shows, chosen in the sidebar
// or in the chips. It is not kept, so that a window opens on every change.
type scope struct {
	kind  scopeKind
	group int // the group shown, -1 for none
	file  int // the file shown, -1 for none
}

// allScope shows every change.
func allScope() scope { return scope{kind: scopeAll, group: -1, file: -1} }

// all reports whether the surface shows every change.
func (s scope) all() bool { return s.kind == scopeAll }

// inScope reports whether the surface shows a file.
func (w *window) inScope(i int) bool {
	switch w.scope.kind {
	case scopeOneGroup:
		return w.files[i].group == w.scope.group
	case scopeOneFile:
		return i == w.scope.file
	}
	return true
}

// the file view of the settings: one file at a time, or every file
// stacked one under the other.
const (
	fileViewOne   = "one"
	fileViewStack = "stack"
)

// oneFile reports whether the surface shows one file at a time, rather
// than every file of the scope one under the other.
func (w *window) oneFile() bool { return w.settings.FileView != fileViewStack }

// viewFiles lists the files the file view may show, in the order they
// show: those of the scope that pass the filter.
func (w *window) viewFiles() []int {
	var out []int
	for i := range w.files {
		if w.fileVisible(i) && w.inScope(i) {
			out = append(out, i)
		}
	}
	return out
}

// shownFile is the file the surface shows alone: the one chosen while it
// is of the scope, else the first of it, -1 for none.
func (w *window) shownFile() int {
	first := -1
	for i := range w.files {
		if !w.fileVisible(i) || !w.inScope(i) {
			continue
		}
		if first < 0 {
			first = i
		}
		if i == w.current {
			return i
		}
	}
	return first
}

// fileNav is where the file shown stands among those the file view may
// show, 1-based, and how many there are.
func (w *window) fileNav() (at, total int) {
	files := w.viewFiles()
	if len(files) == 0 {
		return 0, 0
	}
	return slices.Index(files, w.shownFile()) + 1, len(files)
}

// canStep reports whether a file comes before (dir -1) or after (dir 1)
// the one shown: the ends of the file view stop there, they do not go
// round.
func (w *window) canStep(dir int) bool {
	at, total := w.fileNav()
	return at > 0 && at+dir >= 1 && at+dir <= total
}

// stepFile shows the file before (dir -1) or after (dir 1) the one shown,
// within the scope.
func (w *window) stepFile(dir int) {
	if !w.canStep(dir) {
		return
	}
	files := w.viewFiles()
	i := files[slices.Index(files, w.shownFile())+dir]
	w.current = i
	w.selFile, w.selHunk, w.noteSel = -1, -1, nil
	w.revealedAt = w.now
	w.rowsDirty = true
	w.selectTreeFile(i)
}

// setFileView shows one file at a time (mode fileViewOne), or every file
// stacked (mode fileViewStack), and keeps the choice in the settings.
func (w *window) setFileView(mode string) {
	if mode != fileViewOne && mode != fileViewStack {
		mode = fileViewOne
	}
	if w.settings.FileView == mode {
		return
	}
	w.settings.FileView = mode
	w.selFile, w.selHunk = -1, -1
	w.rowsDirty = true
	// The rows of the other view start from the top of the file shown.
	w.shownFor = -1
	w.list.ScrollTo(0, ui.Start)
	cfg.Update(func(s *Settings) { s.FileView = mode })
}

// summaryFolded reports whether the agent's review folds to one line: it
// summarizes every change, not the part of the review shown.
func (w *window) summaryFolded() bool { return w.oneFile() || !w.scope.all() }

// fileGroup is a group of the files, which show together.
type fileGroup struct {
	key, label string
	summary    string
	critical   bool
	ai         bool
	// category is what part of the system the group touches, as PRD §6.2
	// has it, "" when the analysis does not say yet.
	category string
	// missing are the paths of the group that are no longer in the diff.
	missing []string
}

// kind is a kind of file, as pulls.review's rules sort them.
type kind struct{ key, label, summary string }

// kinds are the kinds of files, in the order their groups show.
var kinds = []kind{
	{"docs", "Docs", "Documentation updates."},
	{"code", "Code", "Source changes."},
	{"tests", "Tests", "Test coverage for the change."},
	{"config", "Config", "Configuration changes."},
	{"deps", "Dependencies", "Dependency version changes."},
	{"generated", "Generated", "Generated or build output changes."},
	{"other", "Other", "Other changes that don't fit an existing category."},
}

// kindOf returns the index in kinds of a file's kind. The first rule that
// matches wins: a document under .github is a document.
func kindOf(f *diff.File) int {
	index := func(key string) int {
		return slices.IndexFunc(kinds, func(k kind) bool { return k.key == key })
	}
	if f.Directory {
		return index("generated")
	}
	if f.Binary {
		return index("other")
	}
	p := strings.ToLower(f.Path)
	parts := strings.Split(p, "/")
	base, dirs := parts[len(parts)-1], parts[:len(parts)-1]
	inDir := func(names ...string) bool {
		for _, d := range dirs {
			if slices.Contains(names, d) {
				return true
			}
		}
		return false
	}
	ext := ""
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		ext = base[i:]
	}
	switch {
	case ext == ".md" || ext == ".mdx" || ext == ".mdc" || ext == ".rst" || ext == ".adoc",
		inDir("docs"), strings.HasPrefix(base, "readme"):
		return index("docs")
	case strings.Contains(base, ".test.") || strings.Contains(base, ".spec."),
		inDir("__tests__", "test", "tests", "testdata"),
		strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_") && ext == ".py",
		strings.HasSuffix(base, "_spec.rb"):
		return index("tests")
	case len(dirs) == 0 && (strings.Contains(base, ".config.") || strings.HasPrefix(base, ".git") ||
		strings.HasPrefix(base, ".") && (strings.HasSuffix(base, "rc") || strings.Contains(base, "rc."))),
		len(dirs) > 0 && dirs[0] == ".github",
		strings.HasPrefix(base, "tsconfig") && ext == ".json",
		base == "dockerfile", base == ".dockerignore", base == "makefile",
		strings.HasPrefix(base, "docker-compose") && (ext == ".yml" || ext == ".yaml"):
		return index("config")
	case slices.Contains([]string{"package.json", "pnpm-workspace.yaml", "cargo.toml", "go.mod", "gemfile", "requirements.txt"}, base):
		return index("deps")
	case f.Generated, inDir("dist"), ext == ".lock", strings.Contains(base, ".generated."):
		return index("generated")
	}
	return index("code")
}

// grouping returns how the files group now: as the user chose, else as
// fits the source.
func (w *window) grouping() groupMode {
	an := w.analysisResult()
	mode := w.groupPref
	if mode == groupAI && an == nil {
		mode = groupAuto
	}
	if !w.settings.GroupingEnabled {
		// Smart grouping is off: the rules group the files alone.
		if mode == groupAI || mode == groupAuto {
			return groupKind
		}
	}
	if mode != groupAuto {
		return mode
	}
	switch {
	case an != nil:
		return groupAI
	case w.source.kind == sourcePull:
		return groupKind
	}
	return groupNone
}

// setGrouping groups the files another way.
func (w *window) setGrouping(mode groupMode) {
	w.groupPref = mode
	w.regroupSoon()
}

// regroupSoon groups the files again as the next frame starts: the list
// may be building rows of the files in their order now.
func (w *window) regroupSoon() {
	w.regroupPending = true
	w.invalidate()
}

// regroup groups the files, and orders them by their groups: the tree,
// the surface and the hunks follow the order.
func (w *window) regroup() {
	path := func(i int) string {
		if i >= 0 && i < len(w.files) {
			return w.files[i].Path
		}
		return ""
	}
	current, selected := path(w.current), path(w.selFile)
	// The scope follows its file or its group to their new place.
	scopePath, scopeKey := "", ""
	if w.scope.kind == scopeOneFile {
		scopePath = path(w.scope.file)
	}
	if w.scope.kind != scopeAll && w.scope.group >= 0 && w.scope.group < len(w.groups) {
		scopeKey = w.groups[w.scope.group].key
	}
	w.groups = nil
	for _, f := range w.files {
		f.group = -1
	}
	switch w.grouping() {
	case groupKind:
		used := make([]int, len(kinds))
		for i := range used {
			used[i] = -1
		}
		ranks := make([]int, len(w.files))
		for i, f := range w.files {
			ranks[i] = kindOf(f.File)
		}
		// The groups in the kinds' order, the empty ones left out.
		for k := range kinds {
			if slices.Contains(ranks, k) {
				used[k] = len(w.groups)
				w.groups = append(w.groups, fileGroup{key: kinds[k].key, label: kinds[k].label, summary: kinds[k].summary})
			}
		}
		for i, f := range w.files {
			f.group = used[ranks[i]]
		}
	case groupAI:
		an := w.analysisResult()
		byPath := map[string]int{}
		for gi, g := range an.Groups {
			w.groups = append(w.groups, fileGroup{key: g.Key, label: g.Label, summary: g.Summary, critical: g.Critical, ai: true,
				category: groupCategory(&g), missing: groupMissing(&g)})
			for _, p := range g.FilePaths {
				if _, ok := byPath[p]; !ok {
					byPath[p] = gi
				}
			}
		}
		rest := -1
		for _, f := range w.files {
			gi, ok := byPath[f.Path]
			if !ok && f.OldPath != f.Path {
				gi, ok = byPath[f.OldPath]
			}
			if !ok {
				// Files new since the review gather at the end.
				if rest < 0 {
					rest = len(w.groups)
					w.groups = append(w.groups, fileGroup{key: "unreviewed", label: "Not in the review", summary: "Files that changed since the agent's review.", ai: true})
				}
				gi = rest
			}
			f.group = gi
		}
		// Groups left without files go, the indices following.
		count := make([]int, len(w.groups))
		for _, f := range w.files {
			count[f.group]++
		}
		remap := make([]int, len(w.groups))
		var kept []fileGroup
		for gi, g := range w.groups {
			remap[gi] = -1
			if count[gi] > 0 {
				remap[gi] = len(kept)
				kept = append(kept, g)
			}
		}
		w.groups = kept
		for _, f := range w.files {
			f.group = remap[f.group]
		}
	}
	slices.SortStableFunc(w.files, func(a, b *fileState) int {
		if a.group != b.group {
			return a.group - b.group
		}
		return git.ComparePaths(a.Path, b.Path)
	})
	w.current, w.selFile = 0, -1
	for i, f := range w.files {
		if f.Path == current {
			w.current = i
		}
		if f.Path == selected {
			w.selFile = i
		}
	}
	if w.selFile < 0 {
		w.selHunk = -1
	}
	// A scope whose group or file is gone shows every change; a file's
	// scope falls back to its group.
	w.scope = allScope()
	if scopeKey != "" {
		if gi := slices.IndexFunc(w.groups, func(g fileGroup) bool { return g.key == scopeKey }); gi >= 0 {
			w.scope = scope{kind: scopeOneGroup, group: gi, file: -1}
		}
	}
	if scopePath != "" {
		if i := slices.IndexFunc(w.files, func(f *fileState) bool { return f.Path == scopePath }); i >= 0 {
			w.scope = scope{kind: scopeOneFile, group: w.files[i].group, file: i}
		}
	}
	if w.scope.kind == scopeOneFile {
		w.current = w.scope.file
	}
	w.buildNotes()
	w.matchesFor = "\x00" // find again
	w.buildTree()
	w.rowsDirty = true
}

// groupFiles returns the indices of a group's files.
func (w *window) groupFiles(gi int) []int {
	var out []int
	for i, f := range w.files {
		if f.group == gi {
			out = append(out, i)
		}
	}
	return out
}

// showAll shows every change again, from its top.
func (w *window) showAll() { w.setScope(allScope()) }

// showGroup shows the files of a group alone. A second click on the group
// shown goes back to every change, as a row folds itself shut.
func (w *window) showGroup(gi int) {
	if gi < 0 || gi >= len(w.groups) {
		return
	}
	if w.scope.kind == scopeOneGroup && w.scope.group == gi {
		w.showAll()
		return
	}
	w.setScope(scope{kind: scopeOneGroup, group: gi, file: -1})
}

// showFile shows one file alone. A second click on the file shown goes back
// to every change.
func (w *window) showFile(i int) {
	if i < 0 || i >= len(w.files) {
		return
	}
	if w.scope.kind == scopeOneFile && w.scope.file == i {
		w.showAll()
		return
	}
	w.setScope(scope{kind: scopeOneFile, group: w.files[i].group, file: i})
}

// scopeUp shows the group a file shown alone belongs to, and every change
// from a group: the steps back the surface's breadcrumb takes.
func (w *window) scopeUp(gi int) {
	if w.scope.kind == scopeOneFile && gi >= 0 {
		w.showGroup(gi)
		return
	}
	w.showAll()
}

// setScope changes what the surface shows: the rows of the new scope from
// its top, the file at the top, and the aside and the file tree with them.
func (w *window) setScope(s scope) {
	w.scope = s
	// The agent's review summarizes every change, so it folds back to a
	// line.
	w.summaryOpen = false
	w.rowsDirty = true
	w.buildTree()
	w.buildRows()
	w.list.ScrollTo(0, ui.Start)
	w.revealedAt = w.now
	switch s.kind {
	case scopeOneFile:
		if s.file >= 0 && s.file < len(w.files) {
			// The aside lists the file's group, the file marked in it.
			w.current = s.file
			w.selectTreeFile(s.file)
			w.asideFor, w.asideAt = w.files[s.file].group, -1
		}
	case scopeOneGroup:
		if files := w.groupFiles(s.group); len(files) > 0 {
			w.current = files[0]
			w.selectTreeFile(files[0])
		}
		w.asideFor, w.asideAt = s.group, -1
	default:
		// The group at the top of the surface, which the aside lists.
		for _, r := range w.rows {
			if !r.banner() {
				if i := int(r.file); i >= 0 && i < len(w.files) {
					w.current = i
					w.selectTreeFile(i)
				}
				break
			}
		}
		w.asideFor, w.asideAt = -1, -1
	}
}

// groupIcon draws the mark of a group: a sparkle for the agent's groups.
func groupIcon(g *fileGroup) *ui.SVG {
	if g.ai {
		return iconSparkle
	}
	return iconLayers
}

// The aside's size: the width it shows at, and the bounds a drag keeps it
// in.
const (
	asideDefault = 240
	asideMin     = 220
	asideMax     = 260
)

// asideBreak is how wide the review beside the sidebar must be to show the
// aside there: narrower, it keeps one column.
const asideBreak = 1100

// groupStripH is the height of the strip heading a group's block, and of
// the band it stands in atop the block's file headers.
const groupStripH = 30

// blockGap spaces the blocks of the groups, above the strip heading one.
const blockGap = 18

// asideOn reports whether the review shows the aside: the files are
// grouped, and the window is wide enough for the diffs beside it.
func (w *window) asideOn() bool {
	return w.grouping() != groupNone && len(w.groups) > 0 && w.columnW >= asideBreak
}

// topGroup returns the group whose files show at the top of the surface,
// which the aside lists.
func (w *window) topGroup() int {
	first, _ := w.list.Visible()
	for first >= 0 && first < len(w.rows) && w.rows[first].banner() {
		first++
	}
	if first >= 0 && first < len(w.rows) {
		if i := int(w.rows[first].file); i >= 0 && i < len(w.files) {
			return w.files[i].group
		}
	}
	if w.current >= 0 && w.current < len(w.files) {
		return w.files[w.current].group
	}
	return -1
}

// groupStats counts how many files of a group were viewed, and the lines
// they add and remove.
func (w *window) groupStats(gi int) (viewed, adds, dels int) {
	for _, i := range w.groupFiles(gi) {
		f := w.files[i]
		if w.isViewed(f) {
			viewed++
		}
		if countable(f) {
			adds += f.Additions
			dels += f.Deletions
		}
	}
	return
}

// groupStrip heads a group's block: its mark, its name, what it changes,
// and how much of it was viewed. It shows above the first file of the
// group, and above the file at the top of the surface, whose header is
// pinned there: the strip rides that header to the top, so the group read
// names itself while its diffs scroll. The list pins one row at a time, so
// a strip of its own row would cost the file's header its place there, and
// a file shown by ScrollTo would hide under it.
func (w *window) groupStrip(c *ui.Context, pal *palette, gi, idx int) {
	t := c.Theme()
	strip := ui.Row(c).Height(groupStripH).Background(pal.appBg)
	if gi < 0 || gi >= len(w.groups) {
		return
	}
	files := w.groupFiles(gi)
	first := len(files) > 0 && files[0] == idx
	if !first && idx != w.current {
		// The band of a file inside the block spaces it from the one above.
		return
	}
	g := &w.groups[gi]
	viewed, adds, dels := w.groupStats(gi)
	allCollapsed := true
	for _, i := range files {
		if !w.files[i].collapsed {
			allCollapsed = false
		}
	}
	collapse := func() {
		for _, i := range files {
			w.files[i].collapsed = !allCollapsed
		}
		w.rowsDirty = true
	}
	strip.Children(func() {
		// The group's name goes back to every change; the counts beside it
		// collapse or expand the block.
		name := ui.ButtonBase(c).Height(groupStripH).Padding(0, 4, 0, 8).Gap(8).Shrink(0).
			Label("All changes, away from " + g.label).Tooltip("Show every change (\u2318\u21e7A)")
		name.FocusRing(false)
		if name.Hovered() {
			name.Background(pal.hover.Alpha(0.7))
		}
		if name.Clicked() {
			w.showAll()
		}
		name.Children(func() {
			icon := ui.Icon(c, groupIcon(g)).FontSize(13).TextColor(t.TextMuted)
			if g.critical {
				icon.TextColor(t.Danger)
			}
			ui.Text(c, g.label).FontSize(13).FontWeight(600).SingleLine().Shrink(0)
			ui.Icon(c, iconChevronRight).FontSize(10).TextColor(t.TextMuted).Shrink(0)
		})
		toggle := ui.ButtonBase(c).Height(groupStripH).Padding(0, 10, 0, 8).Gap(8).Shrink(1).MinWidth(0).
			Label("Collapse or expand " + g.label)
		toggle.FocusRing(false)
		if toggle.Hovered() {
			toggle.Background(pal.hover.Alpha(0.7))
		}
		if toggle.Clicked() {
			collapse()
		}
		toggle.Children(func() {
			ui.RichText(c,
				ui.Span{Text: plural(len(files), "file") + " · ", Color: t.TextMuted},
				ui.Span{Text: "+" + thousands(adds), Color: pal.addText},
				ui.Span{Text: " −" + thousands(dels), Color: pal.delText},
			).FontSize(11).FontWeight(600).Shrink(0)
			ui.Box(c).Width(54).Height(3).Radius(2).Background(pal.pill).Clip().Shrink(0).Children(func() {
				ui.Box(c).WidthPercent(float32(viewed) / float32(len(files)) * 100).Height(3).Radius(2).Background(pal.viewed)
			})
			count := ui.Textf(c, "%d/%d viewed", viewed, len(files)).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			if viewed == len(files) {
				count.TextColor(pal.viewed)
			}
			if g.critical {
				ui.Text(c, "Needs care").FontSize(10).FontWeight(600).Padding(2, 6).Radius(8).
					Background(t.Danger.Alpha(0.14)).TextColor(t.Danger).Shrink(0)
			}
		})
		ui.Spacer(c)
		// The group's own control, for a review of several groups.
		if len(w.groups) > 1 {
			label := "Collapse all"
			if allCollapsed {
				label = "Expand all"
			}
			b := ui.ButtonBase(c).Height(groupStripH).Padding(0, 12, 0, 10).Label(label + " of " + g.label)
			if b.Hovered() {
				b.Background(pal.hover.Alpha(0.7))
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
			})
			if b.Clicked() {
				collapse()
			}
		}
	})
}

// groupAside lists the files of a group beside its diffs, so that the
// files stay in sight while their diffs scroll. It is pinned above the
// surface rather than bound to the block's rows: a row of the surface
// holds no more than its own height, and stops being built as it scrolls
// out, so what the aside shows follows the group at the top instead.
func (w *window) groupAside(c *ui.Context, pal *palette, gi int) {
	t := c.Theme()
	panel := ui.Column(c).Width(w.asideWidth).Shrink(0).Margin(12, 0, 24, 12).Radius(cardRadius).
		Border(1, pal.cardBorder).Background(pal.headerBg).Clip()
	if gi < 0 || gi >= len(w.groups) {
		return
	}
	g := &w.groups[gi]
	files := w.groupFiles(gi)
	if len(files) == 0 {
		return
	}
	if w.asideFor != gi {
		// Another group: its files list from its top, then follow the file
		// read.
		w.asideFor, w.asideAt = gi, -1
		w.asideList.ScrollTo(0, ui.Start)
	}
	if at := slices.Index(files, w.current); at >= 0 && at != w.asideAt {
		// The file read shows in the list, which stays where it was put
		// while that file shows.
		w.asideAt = at
		w.asideList.ScrollIntoView(at)
	}
	viewed, adds, dels := w.groupStats(gi)
	allCollapsed := true
	for _, i := range files {
		if !w.files[i].collapsed {
			allCollapsed = false
		}
	}
	whole := w.asideMore[g.key]
	panel.Children(func() {
		ui.Column(c).Padding(12, 12, 8).Gap(6).Children(func() {
			ui.Row(c).Gap(7).Children(func() {
				icon := ui.Icon(c, groupIcon(g)).FontSize(14).TextColor(t.TextMuted)
				if g.critical {
					icon.TextColor(t.Danger)
				}
				ui.Text(c, g.label).FontSize(13).Bold().SingleLine().Grow(1).Shrink(1).MinWidth(0)
				if g.critical {
					ui.Text(c, "Needs care").FontSize(10).FontWeight(600).Padding(2, 6).Radius(8).
						Background(t.Danger.Alpha(0.14)).TextColor(t.Danger).Shrink(0)
				}
			})
			// The counts, the progress and the count viewed read in one
			// line, the strip above the block saying them too.
			ui.Row(c).Gap(8).Children(func() {
				ui.RichText(c,
					ui.Span{Text: plural(len(files), "file") + " · ", Color: t.TextMuted},
					ui.Span{Text: "+" + thousands(adds), Color: pal.addText},
					ui.Span{Text: " −" + thousands(dels), Color: pal.delText},
				).FontSize(11).FontWeight(600).Shrink(0)
				ui.Box(c).Height(3).Radius(2).Background(pal.pill).Clip().Grow(1).Children(func() {
					ui.Box(c).WidthPercent(float32(viewed) / float32(len(files)) * 100).Height(3).Radius(2).Background(pal.viewed)
				})
				count := ui.Textf(c, "%d/%d", viewed, len(files)).FontSize(11).TextColor(t.TextMuted).Shrink(0).
					Tooltip(itoa(viewed) + " of " + plural(len(files), "file") + " viewed")
				if viewed == len(files) {
					count.TextColor(pal.viewed)
				}
			})
		})
		if g.summary != "" {
			ui.Column(c).Padding(0, 12, 6).Gap(2).Children(func() {
				summary := ui.Text(c, g.summary).FontSize(12).TextColor(t.Text.Alpha(0.75)).Selectable()
				if !whole {
					summary.MaxLines(4)
				}
				if summaryLines(g.summary) > 4 {
					label := "Show more"
					if whole {
						label = "Show less"
					}
					b := ui.ButtonBase(c).Padding(2, 0).AlignSelf(ui.Start).Label(label)
					if b.Hovered() {
						b.TextColor(t.Text)
					}
					b.Children(func() {
						ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
					})
					if b.Clicked() {
						w.asideMore[g.key] = !whole
					}
				}
			})
		}
		ui.Row(c).Gap(4).Padding(0, 10, 6).Children(func() {
			label := "Collapse all"
			if allCollapsed {
				label = "Expand all"
			}
			b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Label(label)
			if b.Hovered() {
				b.Background(pal.hover)
			}
			b.Children(func() {
				ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
			})
			if b.Clicked() {
				for _, i := range files {
					w.files[i].collapsed = !allCollapsed
				}
				w.rowsDirty = true
			}
		})
		ui.List(c, &w.asideList, len(files), func(i int) { w.asideFileRow(c, pal, files[i]) }).
			Grow(1).Padding(0, 6, 6).Label("Files in " + g.label)
	})
}

// asideFileRow is a file of the group in the aside: its name, its counts,
// and how far it is reviewed. A click shows it in the diffs.
func (w *window) asideFileRow(c *ui.Context, pal *palette, i int) {
	t := c.Theme()
	f := w.files[i]
	here := i == w.current
	viewed := w.isViewed(f)
	row := ui.Row(c).MinHeight(30).Padding(0, 10, 0, 8).Gap(6).Radius(6).Cursor(ui.CursorPointer).
		Label(f.Path).Tooltip(f.Path)
	switch {
	case here:
		row.Background(t.Accent.Alpha(0.12))
	case row.Hovered():
		row.Background(pal.hover)
	}
	if row.Clicked() {
		w.revealFile(i)
		// The keys go back to the diffs, which the click showed.
		w.focusList = true
	}
	row.Children(func() {
		fileIcon(c, f, 13)
		name := ui.Text(c, f.Name()).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		switch {
		case here:
			name.FontWeight(600)
		case viewed || f.collapsed:
			name.TextColor(t.TextMuted)
		}
		if countable(f) && (f.Additions > 0 || f.Deletions > 0) {
			ui.Text(c, "+"+thousands(f.Additions)).Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(pal.addText).Shrink(0)
			ui.Text(c, "−"+thousands(f.Deletions)).Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(pal.delText).Shrink(0)
		}
		ui.Text(c, string(rune(f.Status))).Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(t.TextMuted).Shrink(0)
		if len(w.notes[f.Path]) > 0 {
			ui.Box(c).Size(5, 5).Radius(3).Background(t.Accent).Shrink(0).Tooltip("Has notes from the AI review")
		}
		if viewed {
			ui.Icon(c, iconCheck).FontSize(11).TextColor(pal.viewed).Shrink(0)
		}
	})
}

// asideChars is how many characters a line of the summary takes in the
// aside.
const asideChars = 34

// summaryLines estimates how many lines a summary takes in the aside.
func summaryLines(summary string) int {
	return strings.Count(summary, "\n") + (len(summary)+asideChars-1)/asideChars
}

// asideResizer drags the aside wider or narrower, and keeps its width.
func (w *window) asideResizer(c *ui.Context, pal *palette) {
	ui.Box(c).Width(1).Shrink(0).Background(pal.cardBorder).Children(func() {
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-4).Width(9).Cursor(ui.CursorResizeEW)
		if dx, _, held := handle.Dragged(); held {
			w.asideDrag += dx
			w.asideWidth = min(max(w.asideDrag, asideMin), asideMax)
			w.asideMoved = true
		} else if _, _, done := handle.DragEnded(); done && w.asideMoved {
			// The width the drag left is the one kept.
			w.asideMoved = false
			w.asideDrag = w.asideWidth
			go state.setAsideWidth(w.asideWidth)
		}
	})
}

// categoryAccent names the tone of a category's icon: a theme has no color for
// each of the sixteen, so a category picks one of the few it has.
type categoryAccent uint8

const (
	accentMuted categoryAccent = iota
	accentAccent
	accentDanger
	accentWarning
	accentSuccess
	accentRef
)

// categoryColor resolves a tone for a theme.
func categoryColor(a categoryAccent, t *ui.Theme, pal *palette) ui.Color {
	switch a {
	case accentAccent:
		return t.Accent
	case accentDanger:
		return t.Danger
	case accentWarning:
		return t.Warning
	case accentSuccess:
		return t.Success
	case accentRef:
		return pal.ref
	}
	return t.TextMuted
}

// categories are the sixteen kinds of change of PRD §6.2, each with its icon
// and tone. A category decides the icon and the tone alone, never the order of
// the groups.
var categories = map[string]struct {
	icon   *ui.SVG
	accent categoryAccent
}{
	"ui":       {iconSidebar, accentAccent},
	"api":      {iconOpen, accentAccent},
	"core":     {iconLayers, accentAccent},
	"data":     {iconTree, accentAccent},
	"cli":      {iconCommand, accentAccent},
	"security": {iconEye, accentDanger},
	"tests":    {iconCheckCircle, accentSuccess},
	"docs":     {iconFile, accentMuted},
	"examples": {iconSparkle, accentMuted},
	"deps":     {iconPull, accentWarning},
	"build":    {iconRefresh, accentWarning},
	"scripts":  {iconPencil, accentWarning},
	"config":   {iconList, accentRef},
	"i18n":     {iconComment, accentAccent},
	"assets":   {iconFolderOpen, accentRef},
	"other":    {iconFileDiff, accentMuted},
}

// categoryIcon returns the icon and the tone of a category, those of "other"
// for one the analysis does not know.
func categoryIcon(category string) (*ui.SVG, categoryAccent) {
	c, ok := categories[strings.ToLower(strings.TrimSpace(category))]
	if !ok {
		c = categories["other"]
	}
	return c.icon, c.accent
}

// groupCategory is the category of a group of the review: which part of the
// system its change touches, of PRD §6.2's sixteen. A group the analysis says
// nothing about is empty, and keeps the mark of how the group was made.
func groupCategory(g *agent.Group) string {
	return string(g.Category)
}

// groupMissing lists the paths a group holds that are no longer in the diff,
// which the review keeps as missing: the tree lists them, weak and out of
// reach.
func groupMissing(g *agent.Group) []string {
	return g.Missing
}

// groupMark is the icon and the tone that mark a group: those of its category
// once the analysis says it, else the mark of how the group was made.
func groupMark(g *fileGroup) (*ui.SVG, categoryAccent) {
	if g.category != "" {
		return categoryIcon(g.category)
	}
	return groupIcon(g), accentMuted
}

// scopeBar shows every change and the groups as chips, with how many of
// their files were viewed; a chip shows that part of the review alone, and
// the one shown again goes back to every change.
func (w *window) scopeBar(c *ui.Context, pal *palette) {
	if len(w.groups) == 0 {
		return
	}
	t := c.Theme()
	// The chip of the group shown, or of the file shown; every change has
	// its own.
	active := -1
	switch w.scope.kind {
	case scopeOneGroup, scopeOneFile:
		active = w.scope.group
	default:
		if w.current >= 0 && w.current < len(w.files) {
			active = w.files[w.current].group
		}
	}
	viewed := make([]int, len(w.groups))
	total := make([]int, len(w.groups))
	every, everyViewed := 0, 0
	for _, f := range w.files {
		if w.hasContent(f) {
			every++
			if w.isViewed(f) {
				everyViewed++
			}
		}
		if f.group < 0 {
			continue
		}
		total[f.group]++
		if w.isViewed(f) {
			viewed[f.group]++
		}
	}
	ui.ScrollHorizontal(c).Shrink(0).Padding(8, 12, 0).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			all := ui.ButtonBase(c.Key("all")).Height(26).Padding(0, 10, 0, 8).Gap(6).Radius(13).
				Label("All changes").Tooltip("Show every change (\u2318\u21e7A)").
				Border(1, ui.RGBA(127, 127, 127, 0.18)).Shrink(0)
			switch {
			case w.scope.all():
				all.Background(t.Accent.Alpha(0.14)).Border(1, t.Accent.Alpha(0.45))
			case all.Hovered():
				all.Background(pal.hover)
			}
			if all.Clicked() {
				w.showAll()
			}
			all.Children(func() {
				ui.Icon(c, iconFileDiff).FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, "All").FontSize(12).FontWeight(600).SingleLine()
				count := ui.Textf(c, "%d/%d", everyViewed, every).Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(t.TextMuted)
				if everyViewed == every && every > 0 {
					count.TextColor(pal.viewed)
				}
			})
			for gi := range w.groups {
				g := &w.groups[gi]
				tip := g.label
				if g.summary != "" {
					tip += ": " + g.summary
				}
				b := ui.ButtonBase(c.Key(g.key)).Height(26).Padding(0, 10, 0, 8).Gap(6).Radius(13).Label(g.label).Tooltip(tip).
					Border(1, ui.RGBA(127, 127, 127, 0.18)).Shrink(0)
				switch {
				case gi == active:
					b.Background(t.Accent.Alpha(0.14)).Border(1, t.Accent.Alpha(0.45))
				case b.Hovered():
					b.Background(pal.hover)
				}
				if b.Clicked() {
					w.showGroup(gi)
				}
				done := viewed[gi] == total[gi] && total[gi] > 0
				b.Children(func() {
					svg, accent := groupMark(g)
					icon := ui.Icon(c, svg).FontSize(12).TextColor(categoryColor(accent, t, pal))
					if g.critical {
						icon.TextColor(t.Danger)
					}
					ui.Text(c, g.label).FontSize(12).FontWeight(600).SingleLine()
					count := ui.Textf(c, "%d/%d", viewed[gi], total[gi]).Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(t.TextMuted)
					if done {
						count.TextColor(pal.viewed)
					}
				})
			}
		})
	})
}

// groupRow is the header of a group on the surface: its name, why it is,
// and how much of it was viewed.
func (w *window) groupRow(c *ui.Context, pal *palette, gi int) {
	t := c.Theme()
	if gi < 0 || gi >= len(w.groups) {
		ui.Box(c).Height(0)
		return
	}
	if w.asideOn() {
		// The aside heads the group beside its files, so the block needs
		// no band of its own: a gap spaces it from the one above.
		ui.Box(c).Height(blockGap).Background(pal.appBg)
		return
	}
	g := &w.groups[gi]
	files := w.groupFiles(gi)
	viewed, adds, dels := w.groupStats(gi)
	allCollapsed := true
	for _, i := range files {
		if !w.files[i].collapsed {
			allCollapsed = false
		}
	}
	collapse := func(want bool) {
		for _, i := range files {
			w.files[i].collapsed = want
		}
		w.rowsDirty = true
	}
	ui.Column(c).Padding(20, 2, 0).Gap(4).Background(pal.appBg).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			svg, accent := groupMark(g)
			icon := ui.Icon(c, svg).FontSize(15).TextColor(categoryColor(accent, t, pal))
			if g.critical {
				icon.TextColor(t.Danger)
			}
			// The name of the group goes back to every change, as the name on
			// the strip of a block does.
			name := ui.ButtonBase(c).Gap(8).Shrink(1).MinWidth(0).Label("All changes, away from " + g.label).
				Tooltip("Show every change (⌘⇧A)")
			if name.Hovered() {
				name.Background(pal.hover.Alpha(0.7))
			}
			if name.Clicked() {
				w.showAll()
			}
			name.Children(func() {
				ui.Icon(c, svg).FontSize(15).TextColor(categoryColor(accent, t, pal))
				ui.Text(c, g.label).FontSize(15).Bold().SingleLine().Shrink(1).MinWidth(0)
				ui.Icon(c, iconChevronRight).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			})
			if g.critical {
				ui.Text(c, "Needs care").FontSize(11).FontWeight(600).Padding(2, 8).Radius(10).
					Background(t.Danger.Alpha(0.14)).TextColor(t.Danger).Shrink(0)
			}
			ui.RichText(c,
				ui.Span{Text: plural(len(files), "file") + " · ", Color: t.TextMuted},
				ui.Span{Text: "+" + thousands(adds), Color: pal.addText},
				ui.Span{Text: " −" + thousands(dels), Color: pal.delText},
			).FontSize(12).FontWeight(600).Shrink(0)
			// The same bar the sidebar shows this group's progress with, so
			// that both read alike.
			ui.Progress(c, ratio(viewed, len(files))).Width(groupProgressWidth).Shrink(0).AlignSelf(ui.Center)
			ui.Spacer(c)
			count := ui.Textf(c, "%d/%d viewed", viewed, len(files)).FontSize(12).TextColor(t.TextMuted).Shrink(0)
			if viewed == len(files) && viewed > 0 {
				count.TextColor(pal.viewed)
			}
			label := "Collapse"
			if allCollapsed {
				label = "Expand"
			}
			b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Label(label + " " + g.label).Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			})
			if b.Hovered() {
				b.Background(pal.hover)
			}
			if b.Clicked() {
				collapse(!allCollapsed)
			}
			// Every file of the review: the button beside it reaches this
			// group alone, so with one group this would say the same.
			if len(w.groups) > 1 {
				everyCollapsed := w.allCollapsed()
				label := "Collapse all"
				if everyCollapsed {
					label = "Expand all"
				}
				ab := ui.ButtonBase(c).Padding(3, 8).Radius(6).Label(label).Children(func() {
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				})
				if ab.Hovered() {
					ab.Background(pal.hover)
				}
				if ab.Clicked() {
					for _, f := range w.files {
						f.collapsed = !everyCollapsed
					}
					w.rowsDirty = true
				}
			}
		})
		if g.summary != "" {
			ui.Text(c, g.summary).FontSize(13).TextColor(t.Text.Alpha(0.8)).Selectable().Padding(0, 0, 0, 23)
		}
	})
}

// groupProgressWidth is how wide a group's progress shows in a heading of
// the surface.
const groupProgressWidth = 80

// allCollapsed reports whether every file of the review is collapsed.
func (w *window) allCollapsed() bool {
	for _, f := range w.files {
		if !f.collapsed {
			return false
		}
	}
	return len(w.files) > 0
}

// aiNote is a note of the agent's review on a file, or on a line of it.
type aiNote struct {
	path     string
	file     bool // on the whole file
	side     side
	line     int
	text     string
	critical bool
}

// buildNotes gathers the notes of the agent's review on files unchanged
// since: on others, the lines they are on may have moved.
func (w *window) buildNotes() {
	sel := w.noteSel
	w.notes, w.noteSel = nil, nil
	st := w.analyses[w.source]
	if st == nil || st.result == nil || !w.settings.Notes {
		return
	}
	// The note gone to last stays marked.
	defer func() {
		if sel == nil {
			return
		}
		for _, n := range w.notes[sel.path] {
			if *n == *sel {
				w.noteSel = n
			}
		}
	}()
	current := map[string]bool{}
	for _, f := range w.files {
		if fp, ok := st.fingerprints[f.Path]; !ok || fp == f.Fingerprint {
			current[f.Path] = true
		}
	}
	w.notes = map[string][]*aiNote{}
	for _, g := range st.result.Groups {
		for _, n := range g.FileNotes {
			if current[n.Path] {
				w.notes[n.Path] = append(w.notes[n.Path], &aiNote{path: n.Path, file: true, text: n.Text, critical: n.Critical})
			}
		}
		for _, n := range g.LineNotes {
			if !current[n.Path] {
				continue
			}
			sd := sideNew
			if n.Side == "deletions" {
				sd = sideOld
			}
			w.notes[n.Path] = append(w.notes[n.Path], &aiNote{path: n.Path, side: sd, line: n.Line, text: n.Text, critical: n.Critical})
		}
	}
}

// noteRow shows a note of the agent's review, under the line it is on or
// atop the file.
func (w *window) noteRow(c *ui.Context, pal *palette, n *aiNote) {
	t := c.Theme()
	accent := t.Accent
	if n.critical {
		accent = t.Danger
	}
	border := accent.Alpha(0.22)
	if n == w.noteSel {
		// The note gone to last.
		border = accent
	}
	w.card(c, pal).Padding(6, 16).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(10).Padding(8, 12).Radius(10).AlignItems(ui.Start).
			Background(accent.Alpha(0.07)).Border(1, border).Children(func() {
			ui.Icon(c, iconSparkle).FontSize(14).TextColor(accent).Margin(1, 0, 0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				label := "AI note"
				switch {
				case n.file:
					label = "AI note on this file"
				case n.side == sideOld:
					label = "AI note · old line " + itoa(n.line)
				default:
					label = "AI note · line " + itoa(n.line)
				}
				if n.critical {
					label += " · needs care"
				}
				ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(accent)
				ui.Text(c, n.text).FontSize(13).Selectable()
			})
		})
	})
}

// orderedNotes lists the notes of the agent's review in the order their
// files show, those on a whole file before those on its lines.
func (w *window) orderedNotes() []*aiNote {
	var out []*aiNote
	for _, f := range w.files {
		notes := slices.Clone(w.notes[f.Path])
		slices.SortStableFunc(notes, func(a, b *aiNote) int {
			if a.file != b.file {
				if a.file {
					return -1
				}
				return 1
			}
			return a.line - b.line
		})
		out = append(out, notes...)
	}
	return out
}

// nextNote shows the next (dir 1) or the previous (dir -1) note of the
// agent's review, after the one shown last, else after the file at the
// top, and around from the end.
func (w *window) nextNote(dir int) {
	notes := w.orderedNotes()
	if len(notes) == 0 {
		return
	}
	at := slices.Index(notes, w.noteSel)
	if at >= 0 {
		at = (at + dir + len(notes)) % len(notes)
	} else {
		file := map[string]int{}
		for i, f := range w.files {
			file[f.Path] = i
		}
		if dir > 0 {
			at = max(slices.IndexFunc(notes, func(n *aiNote) bool { return file[n.path] >= w.current }), 0)
		} else {
			at = len(notes) - 1
			for i, n := range notes {
				if file[n.path] < w.current {
					at = i
				}
			}
		}
	}
	w.revealNote(notes[at])
}

// revealNote scrolls a note of the agent's review into the middle of the
// surface, opening its file when closed, and marks it.
func (w *window) revealNote(n *aiNote) {
	fi := slices.IndexFunc(w.files, func(f *fileState) bool { return f.Path == n.path })
	if fi < 0 {
		return
	}
	if !w.fileVisible(fi) {
		w.filter = ""
		if !w.fileVisible(fi) {
			w.finding = false
		}
	}
	if f := w.files[fi]; f.collapsed && !w.forceOpen(fi) {
		f.collapsed = false
	}
	// The file gone to is the one shown, before the rows are laid out:
	// the file view lays out that file's rows alone.
	w.current = fi
	w.noteSel = n
	w.selFile, w.selHunk = -1, -1
	w.rowsDirty = true
	w.buildRows()
	for i := range w.rows {
		if r := &w.rows[i]; r.kind == rowAINote && r.note == n {
			w.list.ScrollTo(i, ui.Center)
			break
		}
	}
	w.revealedAt = w.now
	w.selectTreeFile(fi)
}

// analysisFiles describes the files to an agent.
func analysisFiles(files []*fileState) []agent.File {
	out := make([]agent.File, 0, len(files))
	for _, f := range files {
		if f.Directory {
			continue
		}
		status := byte(f.Status)
		if f.Status == diff.Untracked {
			status = 'A'
		}
		out = append(out, agent.File{Path: f.Path, OldPath: f.OldPath, Status: status, Additions: f.Additions, Deletions: f.Deletions,
			Binary: f.Binary, Generated: f.Generated, Patch: patchText(f.File)})
	}
	return out
}

// patchText writes a file's hunks as a unified diff.
func patchText(f *diff.File) string {
	if f.TooLarge || f.Binary {
		return ""
	}
	var b strings.Builder
	for _, h := range f.Hunks {
		b.WriteString("@@ -" + itoa(h.OldStart) + "," + itoa(h.OldLines) + " +" + itoa(h.NewStart) + "," + itoa(h.NewLines) + " @@")
		if h.Section != "" {
			b.WriteString(" " + h.Section)
		}
		b.WriteByte('\n')
		for _, l := range h.Lines {
			switch l.Kind {
			case diff.Add:
				b.WriteByte('+')
			case diff.Del:
				b.WriteByte('-')
			default:
				b.WriteByte(' ')
			}
			b.WriteString(strings.TrimRight(l.Text, "\r\n"))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }

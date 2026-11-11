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

// the file view of the settings: one file at a time, or every file
// stacked one under the other.
const (
	fileViewOne   = "one"
	fileViewStack = "stack"
)

// oneFile reports whether the surface shows one file at a time, rather
// than every file one under the other.
func (w *window) oneFile() bool { return w.settings.FileView != fileViewStack }

// viewFiles lists the files the file view may show, in the order they
// show: those that pass the filter.
func (w *window) viewFiles() []int {
	var out []int
	for i := range w.files {
		if w.fileVisible(i) {
			out = append(out, i)
		}
	}
	return out
}

// shownFile is the file the surface shows alone: the one chosen while it
// passes the filter, else the first that does, -1 for none.
func (w *window) shownFile() int {
	first := -1
	for i := range w.files {
		if !w.fileVisible(i) {
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

// stepFile shows the file before (dir -1) or after (dir 1) the one shown.
func (w *window) stepFile(dir int) {
	if !w.canStep(dir) {
		return
	}
	files := w.viewFiles()
	w.showFile(files[slices.Index(files, w.shownFile())+dir])
}

// nextUnviewed shows the next (dir 1) or the previous (dir -1) file not
// viewed yet, from the one shown: the review is a circle, so the files are
// searched round from the last to the first, and reports whether it found
// one.
func (w *window) nextUnviewed(dir int) bool {
	files := w.viewFiles()
	if len(files) == 0 {
		return false
	}
	at := max(slices.Index(files, w.shownFile()), 0)
	for step := 1; step <= len(files); step++ {
		i := files[((at+dir*step)%len(files)+len(files))%len(files)]
		if w.isViewed(w.files[i]) {
			continue
		}
		w.showFile(i)
		return true
	}
	return false
}

// stepUnviewed steps to a file not viewed yet, and says so in the bar when
// every file of the review is viewed: nothing moves, and nothing opens.
func (w *window) stepUnviewed(dir int) {
	w.unviewedMiss = !w.nextUnviewed(dir)
}

// showFile shows a file of the files: one at a time it is the one laid out,
// and in the stack the surface scrolls to its head.
func (w *window) showFile(i int) {
	if i < 0 || i >= len(w.files) {
		return
	}
	if !w.oneFile() {
		w.revealFile(i)
		return
	}
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
// summarizes every change, not the one file shown.
func (w *window) summaryFolded() bool { return w.oneFile() }

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

// kind is a kind of file, as pulls.review's rules sort them. Its name and
// summary are the catalog's, said in the interface's language.
type kind struct{ key string }

// kinds are the kinds of files, in the order their groups show.
var kinds = []kind{
	{"docs"},
	{"code"},
	{"tests"},
	{"config"},
	{"deps"},
	{"generated"},
	{"other"},
}

// label names a kind of file.
func (k kind) label() string {
	switch k.key {
	case "docs":
		return msg().GroupDocs()
	case "code":
		return msg().GroupCode()
	case "tests":
		return msg().GroupTests()
	case "config":
		return msg().GroupConfig()
	case "deps":
		return msg().GroupDeps()
	case "generated":
		return msg().GroupGenerated()
	}
	return msg().GroupOther()
}

// summary says what a kind of file is.
func (k kind) summary() string {
	switch k.key {
	case "docs":
		return msg().GroupDocsSummary()
	case "code":
		return msg().GroupCodeSummary()
	case "tests":
		return msg().GroupTestsSummary()
	case "config":
		return msg().GroupConfigSummary()
	case "deps":
		return msg().GroupDepsSummary()
	case "generated":
		return msg().GroupGeneratedSummary()
	}
	return msg().GroupOtherSummary()
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
				w.groups = append(w.groups, fileGroup{key: kinds[k].key, label: kinds[k].label(), summary: kinds[k].summary()})
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
					w.groups = append(w.groups, fileGroup{key: "unreviewed", label: msg().NotInReview(), summary: msg().NotInReviewSummary(), ai: true})
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

// showGroup goes to a group: every file stacked scrolls to the first file
// of the group, and one file at a time shows it. It is a way to a group,
// not a state of the surface: nothing is selected, and nothing folds.
func (w *window) showGroup(gi int) {
	if gi < 0 || gi >= len(w.groups) {
		return
	}
	files := w.groupFiles(gi)
	if len(files) == 0 {
		return
	}
	w.rowsDirty = true
	w.revealFile(files[0])
}

// groupIcon draws the mark of a group: a sparkle for the agent's groups.
func groupIcon(g *fileGroup) *ui.SVG {
	if g.ai {
		return iconSparkle
	}
	return iconLayers
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

// groupChips shows the groups as chips, with how many of their files were
// viewed; a chip goes to its group. They show where the sidebar's tree does
// not, and the chip of the file shown says where the surface is.
func (w *window) groupChips(c *ui.Context, pal *palette) {
	if len(w.groups) == 0 {
		return
	}
	t := c.Theme()
	active := -1
	if w.current >= 0 && w.current < len(w.files) {
		active = w.files[w.current].group
	}
	viewed := make([]int, len(w.groups))
	total := make([]int, len(w.groups))
	for _, f := range w.files {
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
			for gi := range w.groups {
				g := &w.groups[gi]
				tip := g.label
				if g.summary != "" {
					tip += ": " + g.summary
				}
				b := ui.ButtonBase(c.Key(g.key)).Height(26).Padding(0, 10, 0, 8).Gap(6).Radius(radiusChip).Label(g.label).Tooltip(tip).
					Border(1, ui.RGBA(127, 127, 127, 0.18)).Shrink(0)
				switch {
				case gi == active:
					// The group the file shown belongs to: a low ground, and the
					// body color kept, as the selected rows of the tree.
					b.Background(pal.selection)
				case b.Hovered():
					b.Background(pal.surfaceHover)
				}
				if b.Clicked() {
					w.showGroup(gi)
				}
				done := viewed[gi] == total[gi] && total[gi] > 0
				b.Children(func() {
					svg, accent := groupMark(g)
					icon := uiIcon(c, svg, iconSmall).TextColor(categoryColor(accent, t, pal))
					if g.critical {
						icon.TextColor(careTone(pal))
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
	ui.Column(c).Padding(gapBlock, 2, 0).Gap(4).Background(pal.appBg).Children(func() {
		// The whole band folds the group, as its files fold away in the tree:
		// the button at the end says the same, and the chevron only says which
		// way the fold goes.
		head := ui.Row(c).Height(lineGroup).Gap(8).Label(g.label).Tooltip(msg().FoldGroupTip(g.label))
		if head.Hovered() {
			head.Background(pal.surfaceHover.Alpha(0.7))
		}
		if head.Clicked() {
			collapse(!allCollapsed)
		}
		head.Children(func() {
			svg, accent := groupMark(g)
			icon := uiIcon(c, svg, iconBase).TextColor(categoryColor(accent, t, pal))
			if g.critical {
				icon.TextColor(careTone(pal))
			}
			name := ui.Row(c).Gap(8).Shrink(1).MinWidth(0)
			name.Children(func() {
				uiIcon(c, svg, iconBase).TextColor(categoryColor(accent, t, pal))
				ui.Text(c, g.label).FontSize(15).Bold().SingleLine().Shrink(1).MinWidth(0)
				chevron := iconChevronDown
				if allCollapsed {
					chevron = iconChevronRight
				}
				uiIcon(c, chevron, iconBase).TextColor(t.TextMuted).Shrink(0)
			})
			if g.critical {
				ui.Text(c, msg().NeedsCare()).FontSize(11).FontWeight(600).Padding(2, 8).Radius(10).
					Background(careTone(pal).Alpha(0.14)).TextColor(careTone(pal)).Shrink(0)
			}
			ui.RichText(c,
				ui.Span{Text: msg().FilesCount(len(files)) + " · ", Color: t.TextMuted},
				ui.Span{Text: "+" + thousands(adds), Color: pal.addText},
				ui.Span{Text: " −" + thousands(dels), Color: pal.delText},
			).FontSize(12).FontWeight(600).Shrink(0)
			// The same bar the sidebar shows this group's progress with, so
			// that both read alike.
			progressBar(c, pal, ratio(viewed, len(files))).Width(groupProgressWidth).Shrink(0).AlignSelf(ui.Center)
			ui.Spacer(c)
			count := ui.Text(c, msg().GroupViewed(viewed, len(files))).FontSize(12).TextColor(t.TextMuted).Shrink(0)
			if viewed == len(files) && viewed > 0 {
				count.TextColor(pal.viewed)
			}
			label := msg().FoldGroup()
			if allCollapsed {
				label = msg().UnfoldGroup()
			}
			b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Label(label + " " + g.label).Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
			})
			if b.Hovered() {
				b.Background(pal.surfaceHover)
			}
			if b.Clicked() {
				collapse(!allCollapsed)
			}
			// Every file of the review: the button beside it reaches this
			// group alone, so with one group this would say the same.
			if len(w.groups) > 1 {
				everyCollapsed := w.allCollapsed()
				label := msg().FoldAll()
				if everyCollapsed {
					label = msg().UnfoldAll()
				}
				ab := ui.ButtonBase(c).Padding(3, 8).Radius(6).Label(label).Children(func() {
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				})
				if ab.Hovered() {
					ab.Background(pal.surfaceHover)
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
			ui.Text(c, g.summary).FontSize(13).TextColor(t.Text).Selectable().Padding(0, 0, 0, 23)
		}
	})
}

// progressBar shows how much of a group was viewed, in the neutral tones of
// the review: the accent is kept for what can be pressed, and the item that
// is current.
func progressBar(c *ui.Context, pal *palette, share float64) ui.Element {
	const height = 6
	bar := ui.Row(c).Height(height).Radius(height / 2).Background(pal.pressed).Clip().Label(msg().ViewedFilesTip())
	bar.Children(func() {
		ui.Box(c).Height(height).Radius(height / 2).Background(pal.progress).WidthPercent(float32(share) * 100)
	})
	return bar
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
		accent = careTone(pal)
	}
	border := accent.Alpha(0.22)
	if n == w.noteSel {
		// The note gone to last.
		border = accent
	}
	w.card(c, pal).Padding(6, 16).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(10).Padding(8, 12).Radius(10).AlignItems(ui.Start).
			Background(accent.Alpha(0.07)).Border(1, border).Children(func() {
			uiIcon(c, iconSparkle, iconSmall).TextColor(accent).Margin(1, 0, 0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				label := msg().AiNote()
				switch {
				case n.file:
					label = msg().AiNoteOnFile()
				case n.side == sideOld:
					label = msg().AiNoteOldLine(n.line)
				default:
					label = msg().AiNoteLine(n.line)
				}
				if n.critical {
					label += msg().NeedsCareSuffix()
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

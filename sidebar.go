package main

import (
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// treeNode is a row of the file tree: a file, or a directory with the
// chain of directories holding only it compacted into one row, as
// src/app/components.
type treeNode struct {
	name     string
	dir      bool
	file     int    // the file's index in window.files
	path     string // a directory's, or a path no longer in the diff
	group    int    // a group's index, -1 for the others
	children []string
	// missing marks a path the review groups that is no longer in the diff:
	// it shows weak, and going to it goes nowhere.
	missing bool
}

// The sidebar's width: its default, its bounds, and the width below which
// dragging hides it.
const (
	sidebarDefault  = 292
	sidebarMin      = 220
	sidebarMax      = 640
	sidebarCollapse = 80
)

// fuzzyMatch reports whether the runes of query appear in s in order.
func fuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	s, query = strings.ToLower(s), strings.ToLower(query)
	i := 0
	for _, r := range s {
		if i < len(query) && r == rune(query[i]) {
			i++
		}
	}
	return i >= len(query)
}

// fileVisible reports whether a file shows: it passes the filter and,
// while finding, holds a match.
func (w *window) fileVisible(i int) bool {
	f := w.files[i]
	if !w.hasContent(f) {
		return false
	}
	if !fuzzyMatch(f.Path, strings.TrimSpace(w.filter)) {
		return false
	}
	if w.finding && strings.TrimSpace(w.query) != "" {
		return w.fileMatches[i]
	}
	return true
}

// hasContent reports whether a file has something to show: files whose
// only changes were whitespace, hidden, have nothing.
func (w *window) hasContent(f *fileState) bool {
	return len(f.Hunks) > 0 || f.Binary || f.Directory || f.TooLarge || f.Note != "" ||
		f.Status != diff.Modified || f.OldPath != f.Path || f.ModeChange != ""
}

// fileNote returns why a file's lines do not show, "" when they do.
func (w *window) fileNote(f *fileState) string {
	switch {
	case f.Note != "":
		return f.Note
	case f.Binary:
		return msg().FileBinary()
	case f.TooLarge:
		return msg().FileTooLarge()
	case len(f.Hunks) == 0 && f.OldPath != f.Path:
		return msg().FileRenamed()
	case len(f.Hunks) == 0 && f.ModeChange != "":
		return msg().FileModeChanged(f.ModeChange)
	case len(f.Hunks) == 0 && f.Status == diff.Added, len(f.Hunks) == 0 && f.Status == diff.Untracked:
		return msg().FileEmptyAdded()
	case len(f.Hunks) == 0 && f.Status == diff.Deleted:
		return msg().FileEmptyDeleted()
	case len(f.Hunks) == 0:
		return msg().FileNoChanges()
	}
	return ""
}

// buildTree lays out the tree of the files that show: under their groups,
// when the files are grouped.
func (w *window) buildTree() {
	type dirNode struct {
		dirs  map[string]*dirNode
		order []string // names of dirs and files, in the files' order
		files map[string]int
	}
	newDir := func() *dirNode { return &dirNode{dirs: map[string]*dirNode{}, files: map[string]int{}} }
	items := map[string]*treeNode{}
	// tree builds the directories of some files, their keys made unique
	// by prefix.
	tree := func(files []int, prefix string) []string {
		root := newDir()
		for _, i := range files {
			f := w.files[i]
			parts := strings.Split(f.Path, "/")
			d := root
			for _, p := range parts[:len(parts)-1] {
				next := d.dirs[p]
				if next == nil {
					next = newDir()
					d.dirs[p] = next
					d.order = append(d.order, "d"+p)
				}
				d = next
			}
			name := parts[len(parts)-1]
			d.files[name] = i
			d.order = append(d.order, "f"+name)
		}
		var walk func(d *dirNode, path string) []string
		walk = func(d *dirNode, path string) []string {
			var keys []string
			for _, o := range d.order {
				name := o[1:]
				if o[0] == 'f' {
					i := d.files[name]
					key := "f:" + w.files[i].Path
					items[key] = &treeNode{name: name, file: i, group: -1}
					keys = append(keys, key)
					continue
				}
				sub := d.dirs[name]
				label := name
				dir := path + name
				// Directories holding one directory alone join it.
				for len(sub.order) == 1 && sub.order[0][0] == 'd' {
					child := sub.order[0][1:]
					label += "/" + child
					dir += "/" + child
					sub = sub.dirs[child]
				}
				key := "d:" + prefix + dir
				items[key] = &treeNode{name: label, dir: true, file: -1, path: dir, group: -1, children: walk(sub, dir+"/")}
				keys = append(keys, key)
			}
			return keys
		}
		return walk(root, "")
	}
	byGroup := make([][]int, len(w.groups))
	var ungrouped []int
	for i, f := range w.files {
		if !w.fileVisible(i) {
			continue
		}
		if f.group >= 0 && f.group < len(w.groups) {
			byGroup[f.group] = append(byGroup[f.group], i)
		} else {
			ungrouped = append(ungrouped, i)
		}
	}
	var roots []string
	for gi, files := range byGroup {
		g := w.groups[gi]
		// A group whose paths the diff no longer holds still shows them.
		if len(files) == 0 && len(g.missing) == 0 {
			continue
		}
		key := "g:" + g.key
		children := tree(files, g.key+":")
		// The paths the review groups that the diff no longer holds stay
		// at the end of the group, out of reach.
		here := map[string]bool{}
		for _, i := range files {
			here[w.files[i].Path] = true
		}
		for _, p := range g.missing {
			if here[p] {
				// The diff holds it after all: the review is stale there.
				continue
			}
			k := "m:" + g.key + ":" + p
			items[k] = &treeNode{name: baseName(p), file: -1, path: p, group: gi, missing: true}
			children = append(children, k)
		}
		items[key] = &treeNode{name: g.label, dir: true, file: -1, group: gi, children: children}
		roots = append(roots, key)
	}
	roots = append(roots, tree(ungrouped, "")...)
	w.treeRoots = roots
	w.treeItems = items
}

// baseName is the last element of a path, as a row of the tree shows it.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// statusColor is the color of a file's status letter.
func statusColor(s diff.Status, pal *palette, t *ui.Theme) ui.Color {
	switch s {
	case diff.Added, diff.Untracked:
		return pal.addBar
	case diff.Deleted:
		return pal.delBar
	case diff.Renamed, diff.Copied:
		return t.Accent
	case diff.Conflict:
		return t.Danger
	}
	return pal.warning
}

func statusLetter(f *fileState) string {
	switch f.Status {
	case diff.Untracked:
		return "U"
	case diff.Conflict:
		return "!"
	}
	return string(f.Status)
}

// compact writes a count briefly: 999, 1.2k, 15k, 1.2m.
func compact(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	case n < 1000000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000000), ".0") + "m"
}

// thousands writes a count with separators: 1,234.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// countable reports whether a file's lines were counted.
func countable(f *fileState) bool {
	return !f.Binary && !f.Directory && !f.TooLarge
}

func (w *window) sidebar(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	bar := c.TitleBar()
	sidebarRight := w.settings.SidebarPosition == "right"
	left, right := max(bar.Left+6, 10), float32(10)
	if sidebarRight {
		left, right = 10, max(bar.Right+6, 10)
	}
	ui.Column(c).Width(w.sidebarWidth).Shrink(0).Background(w.sidebarBg(t)).Children(func() {
		// The title bar, under the window controls: the views of the
		// sidebar and its toggle, centered on the window controls. They
		// keep to the right, clear of the traffic lights, on macOS, and
		// to the left elsewhere, where the window buttons are on the right.
		mac := runtime.GOOS == "darwin"
		ui.Row(c).Height(titleBarHeight).Padding(0, right, 0, left).Gap(6).DragWindow().Children(func() {
			if mac {
				ui.Spacer(c)
			}
			if w.tabControl(c) {
				w.commitOpen = false
			}
			if !mac {
				ui.Spacer(c)
			}
		})
		if w.tab == 2 && !w.gh.ok() {
			w.tab = 0
		}
		ui.Column(c).Padding(2, 10, 8).Children(func() {
			switch w.tab {
			case 0:
				if searchInput(c, &w.filter, msg().FilterFiles(), &w.filterFocus, &w.typing) {
					w.matchesFor = "\x00"
					w.buildTree()
					w.rowsDirty = true
				}
			case 1:
				searchInput(c, &w.historyFilter, msg().FilterHistory(), &w.filterFocus, &w.typing)
			default:
				searchInput(c, &w.pullsFilter, msg().FilterPullRequests(), &w.filterFocus, &w.typing)
			}
		})
		if w.tab == 0 {
			w.foldAllRow(c, pal)
		}
		switch w.tab {
		case 0:
			w.fileTree(c)
		case 1:
			w.historyView(c)
		default:
			w.pullsView(c)
		}
		w.sidebarFooter(c, pal)
	})
}

// sidebarBg is the sidebar's background: none on macOS, where the
// window's material shows through, else the panel's own ground.
func (w *window) sidebarBg(t *ui.Theme) ui.Color {
	if runtime.GOOS == "darwin" && w.win != nil {
		return ui.Transparent
	}
	return paletteFor(t).surface
}

// titleBarHeight is the height of the bars along the top, which centers
// their controls on the traffic lights of a window with an inset title bar.
const titleBarHeight = 52

// tabControl switches the sidebar between the files, the history and the
// pull requests, and reports a switch.
func (w *window) tabControl(c *ui.Context) bool {
	t := c.Theme()
	pal := paletteFor(t)
	type item struct {
		icon *ui.SVG
		name string
	}
	items := []item{{iconTree, msg().TabFiles()}, {iconHistory, msg().TabHistory()}}
	if w.gh.ok() {
		items = append(items, item{iconPull, msg().TabPullRequests()})
	}
	seg := ui.SegmentedBase(c, &w.tab, len(items))
	seg.Track.Padding(2).Gap(2).Radius(8).Background(ui.RGBA(127, 127, 127, 0.12)).Label(msg().SidebarLabel()).Children(func() {
		for i, it := range items {
			s := seg.Segment(i).Size(30, 24).Radius(6).Center().Label(it.name).Tooltip(it.name).TextColor(t.TextMuted)
			if i == w.tab {
				s.Background(pal.surface).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12)).TextColor(t.Text)
			}
			s.Children(func() { ui.Icon(c, it.icon).FontSize(15) })
		}
	})
	return seg.Track.Changed()
}

// sidebarToggle shows and hides the sidebar.
func (w *window) sidebarToggle(c *ui.Context) {
	tip := msg().HideSidebar()
	if !w.sidebarShown {
		tip = msg().ShowSidebar()
	}
	if iconButton(c, iconSidebar, tip).Clicked() {
		w.toggleSidebar()
	}
}

func (w *window) toggleSidebar() {
	w.sidebarShown = !w.sidebarShown
	if w.sidebarShown && w.sidebarWidth < sidebarMin {
		w.sidebarWidth = sidebarDefault
	}
	go state.setLayout(w.sidebarWidth, w.sidebarShown)
}

// searchInput is a field filtering a list, as the search fields of
// macOS: a magnifying glass, the text, and a button clearing it, as Escape
// does. It reports a change.
func searchInput(c *ui.Context, query *string, placeholder string, focus *bool, typing *bool) bool {
	t := c.Theme()
	changed := false
	box := ui.Row(c).Height(28).Padding(0, 6, 0, 8).Gap(6).Radius(7).Background(ui.RGBA(127, 127, 127, 0.12))
	box.Children(func() {
		ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
		in := ui.TextInputBase(c, query).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0)
		if *focus {
			in.Focus()
			*focus = false
		}
		if in.Changed() {
			changed = true
		}
		if in.Focused() {
			box.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45))
			*typing = true
		}
		if *query != "" {
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Background(t.TextMuted.Alpha(0.45)).Label(msg().ClearLabel()).FocusRing(false)
			clear.Children(func() { ui.Icon(c, iconClose).FontSize(10).TextColor(t.Background) })
			if clear.Clicked() || in.Shortcut(0, ui.KeyEscape) {
				*query = ""
				changed = true
			}
		}
	})
	return changed
}

// iconButton is a button showing an icon alone, as in a toolbar.
func iconButton(c *ui.Context, svg *ui.SVG, tip string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip).TextColor(t.TextMuted)
	if b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.22))
	} else if b.Hovered() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Children(func() { ui.Icon(c, svg).FontSize(16) })
	return b
}

// treeRow is a row of the file tree as it shows: an item and its depth.
type treeRow struct {
	key   string
	depth int
}

// visibleTree lists the rows of the tree, inside the directories open: the
// groups, and the files of each under it.
func (w *window) visibleTree() []treeRow {
	var rows []treeRow
	var walk func(keys []string, depth int)
	walk = func(keys []string, depth int) {
		for _, k := range keys {
			rows = append(rows, treeRow{k, depth})
			if n := w.treeItems[k]; n != nil && n.dir && !w.closedDirs[k] {
				walk(n.children, depth+1)
			}
		}
	}
	walk(w.treeRoots, 0)
	return rows
}

// fileTree shows the sidebar's files tab: the groups of the files, and the
// files of each under it. The whole is one list, scrolled as a whole: a
// group folds its files away, which is how much room it takes is decided.
func (w *window) fileTree(c *ui.Context) {
	pal := paletteFor(c.Theme())
	if len(w.treeRoots) == 0 {
		box := emptyBox{shape: emptySmall, icon: iconTree, title: textNoChangesDir()}
		switch {
		case w.loading && len(w.files) == 0:
			box.title, box.detail = msg().Loading(), abbreviateHome(w.repo.Root)
		case strings.TrimSpace(w.filter) != "":
			box.title, box.detail = textNoMatchFiles(), strings.TrimSpace(w.filter)
		}
		emptyState(c, pal, box)
		return
	}
	rows := w.visibleTree()
	w.treeRows = rows
	w.fileTreeList(c, pal, rows)
}

// ratio is a share between 0 and 1, 0 when there is nothing to share.
func ratio(n, of int) float64 {
	if of <= 0 {
		return 0
	}
	return float64(n) / float64(of)
}

// fileTreeList lists rows of the tree.
func (w *window) fileTreeList(c *ui.Context, pal *palette, rows []treeRow) {
	t := c.Theme()
	choose := func(key string) {
		n := w.treeItems[key]
		if n == nil || n.missing {
			// A path no longer in the diff goes nowhere.
			return
		}
		w.treeSel = key
		if !n.dir {
			w.commitOpen = false
			w.revealFile(n.file)
		}
	}
	w.treeList.Key = func(i int) any { return rows[i].key }
	w.treeList.Label = func(i int) string {
		if n := w.treeItems[rows[i].key]; n != nil {
			return n.name
		}
		return ""
	}
	tree := ui.List(c, &w.treeList, len(rows), func(i int) {
		focused := w.treeList.FocusWithin(c)
		key := rows[i].key
		n := w.treeItems[key]
		if n == nil {
			return
		}
		if n.dir && n.group >= 0 && n.group < len(w.groups) {
			// A group heads its files, which fold away under it. It returns
			// before the press and the click below, which choose a row: a
			// group is a switch, and a click on it chooses nothing.
			w.groupTreeRow(c, pal, n.group, key, focused)
			return
		}
		selected := key == w.treeSel
		row := ui.Row(c).Height(lineFile).Padding(0, 8, 0, 6+float32(rows[i].depth)*14).Gap(5).Radius(6).MinWidth(0)
		textColor := t.Text
		switch {
		case selected && focused:
			// The row the keys are on: a low ground, and the body color, so
			// that the accent stays for what can be pressed.
			row.Background(pal.selection)
		case selected:
			row.Background(pal.surfaceHover)
		case row.Hovered():
			row.Background(pal.hover)
		}
		if row.Pressed() && !n.missing && (!selected || w.commitOpen) {
			choose(key)
		}
		if row.Clicked() && !n.missing {
			if n.dir {
				w.closedDirs[key] = !w.closedDirs[key]
				choose(key)
				return
			}
			// A file shows: one file at a time shows its card, and the
			// stacked view scrolls to it. A second click shows it again.
			choose(key)
		}
		muted := t.TextMuted
		row.TextColor(textColor).Children(func() {
			arrow := ui.Box(c).Size(14, 14).Center().Shrink(0)
			if n.dir {
				arrow.Children(func() {
					ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(muted)
					target := float32(0)
					if w.closedDirs[key] {
						target = -90
					}
					ic.Rotate(ic.Animate("rot", target, 150*time.Millisecond))
				})
				ui.Icon(c, iconFolder).FontSize(14).TextColor(muted)
				ui.Text(c, n.name).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				return
			}
			if n.missing {
				// A path the review groups, which the diff no longer holds.
				ui.Icon(c, iconXCircle).FontSize(12).TextColor(muted).Shrink(0)
				ui.Text(c, n.name).Font(w.codeFont()).FontSize(12).TextColor(muted).SingleLine().Grow(1).Shrink(1).MinWidth(0).Tooltip(n.path)
				ui.Text(c, msg().NotInDiff()).FontSize(10).TextColor(muted).Shrink(0).
					Tooltip(msg().NotInDiffTip())
				return
			}
			f := w.files[n.file]
			viewed := w.isViewed(f)
			fileIcon(c, f, 14)
			name := ui.Text(c, n.name).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if viewed && !(selected && focused) {
				name.TextColor(t.TextMuted)
			}
			if notes := w.notes[f.Path]; len(notes) > 0 {
				// The file has notes of the agent's review.
				color := t.Accent
				if slices.ContainsFunc(notes, func(n *aiNote) bool { return n.critical }) {
					color = careTone(pal)
				}
				ui.Icon(c, iconSparkle).FontSize(11).TextColor(color).Shrink(0).Tooltip(msg().AiNotesCount(len(notes)))
			}
			if countable(f) && (f.Additions > 0 || f.Deletions > 0) {
				ui.Textf(c, "+%s -%s", compact(f.Additions), compact(f.Deletions)).
					Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(muted).Shrink(0).
					Tooltip(msg().ChangedLinesTip(f.Additions, f.Deletions))
			}
			letter := statusColor(f.Status, pal, t)
			switch {
			case viewed:
				letter = t.TextMuted
			case w.reloaded[f.Path]:
				letter = pal.ref
			}
			ui.Text(c, statusLetter(f)).Font(w.codeFont()).FontSize(11).FontWeight(700).TextColor(letter).
				Width(12).TextAlign(ui.Center).Shrink(0).Tooltip(textStatus(f.Status))
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().Label(msg().ChangedFilesLabel())
	focusRing(tree, pal, 6)

	// The keys move the choice, open and close directories.
	at := -1
	for i, r := range rows {
		if r.key == w.treeSel {
			at = i
		}
	}
	move := func(i int) {
		if i >= 0 && i < len(rows) {
			choose(rows[i].key)
			w.treeList.ScrollIntoView(i)
		}
	}
	if w.treeList.Shortcut(c, 0, ui.KeyDown) {
		move(at + 1)
	}
	if w.treeList.Shortcut(c, 0, ui.KeyUp) {
		move(max(at-1, 0))
	}
	// Enter on a group shows that part of the review. A file's Enter stays
	// the review's: it comments on the hunk chosen there.
	if at >= 0 {
		if n := w.treeItems[rows[at].key]; n != nil && n.dir && n.group >= 0 && w.treeList.Shortcut(c, 0, ui.KeyEnter) {
			w.showGroup(n.group)
		}
	}
	if w.treeList.Shortcut(c, 0, ui.KeyHome) {
		move(0)
	}
	if w.treeList.Shortcut(c, 0, ui.KeyEnd) {
		move(len(rows) - 1)
	}
	if at >= 0 {
		n := w.treeItems[rows[at].key]
		if w.treeList.Shortcut(c, 0, ui.KeyRight) && n != nil && n.dir {
			if w.closedDirs[rows[at].key] {
				w.closedDirs[rows[at].key] = false
			} else {
				move(at + 1)
			}
		}
		if w.treeList.Shortcut(c, 0, ui.KeyLeft) {
			if n != nil && n.dir && !w.closedDirs[rows[at].key] {
				w.closedDirs[rows[at].key] = true
			} else {
				// To the directory holding it.
				for j := at - 1; j >= 0; j-- {
					if rows[j].depth < rows[at].depth {
						move(j)
						break
					}
				}
			}
		}
	}
}

// groupTreeRow is a group in the sidebar: the heading of the files under
// it, with how much of it was viewed. The whole row folds its files away,
// and the chevron only says which way the fold goes.
//
// A group is a switch, not a row a click may choose: a click writes nothing
// but the fold. It writes no treeSel, so no group takes a selected ground
// and the file the surface showed stays the chosen one; it keeps the focus
// where it was, so the keys stay on the diffs; and it leaves w.focusList
// alone, so it never asks the list for the focus. The keys still say which
// row they are on, with a ring rather than a ground.
func (w *window) groupTreeRow(c *ui.Context, pal *palette, gi int, key string, focused bool) {
	t := c.Theme()
	g := &w.groups[gi]
	files := w.groupFiles(gi)
	viewed, _, _ := w.groupStats(gi)
	open := !w.closedDirs[key]
	row := ui.Row(c).MinHeight(lineGroup).Padding(2, 8, 2, 4).Gap(6).Radius(6).
		AlignItems(ui.Center).MinWidth(0).Label(g.label).KeepFocus()
	switch {
	case key == w.treeSel && focused:
		// The keys moved onto the group: a ring says so, where a ground
		// would read as a choice the user did not make.
		row.Border(1, ui.RGBA(127, 127, 127, 0.45))
	case row.Hovered():
		row.Background(pal.surfaceHover)
	}
	if row.Clicked() {
		w.closedDirs[key] = open
		// The fold folds the group's cards too, where the surface stacks
		// them: one click, one meaning, read in the tree or in the diffs.
		// One file at a time, there is nothing to stack and the card of the
		// file read is left open.
		if !w.oneFile() {
			// A fold is no navigation: the file read stays the one read,
			// and the tree keeps the choice it had, whatever the cards
			// folding away do to the top of the surface.
			file, chosen := w.current, w.treeSel
			for _, i := range w.groupFiles(gi) {
				w.files[i].collapsed = open
			}
			w.current, w.treeSel = file, chosen
			w.revealedAt = w.now
			w.rowsDirty = true
		}
	}
	row.Children(func() {
		tip := msg().HideGroupFiles(g.label)
		if !open {
			tip = msg().ShowGroupFiles(g.label)
		}
		// The chevron says which way the fold goes, and takes no click of its
		// own: a click on it is a click on the row, one fold per click.
		arrow := ui.Box(c).Size(12, 12).Center().Shrink(0).Label(tip).Tooltip(tip)
		arrow.Children(func() {
			ic := ui.Icon(c, iconChevronDown).FontSize(11).TextColor(t.TextMuted)
			target := float32(0)
			if !open {
				target = -90
			}
			ic.Rotate(ic.Animate("grp"+g.key, target, 150*time.Millisecond))
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Row(c).Gap(5).MinWidth(0).AlignItems(ui.Center).Children(func() {
				svg, accent := groupMark(g)
				icon := ui.Icon(c, svg).FontSize(13).TextColor(categoryColor(accent, t, pal)).Shrink(0)
				if g.critical {
					icon.TextColor(careTone(pal))
				}
				ui.Text(c, g.label).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).
					MinWidth(0).Tooltip(g.summary)
				if g.critical {
					ui.Icon(c, iconAlert).FontSize(11).TextColor(careTone(pal)).Shrink(0).Tooltip(msg().NeedsCare())
				}
				count := ui.Textf(c, "%d/%d", viewed, len(files)).Font(w.codeFont()).FontSize(10).
					FontWeight(600).TextColor(t.TextMuted).Shrink(0).Tooltip(msg().ViewedFilesTip())
				if viewed == len(files) && len(files) > 0 {
					count.TextColor(pal.viewed)
				}
				progressBar(c, pal, ratio(viewed, len(files))).Width(40).Shrink(0)
			})
		})
	})
}

// selectTreeFile chooses a file's row in the tree, as the surface
// scrolls to it. A group the user folded stays folded: what the surface
// shows does not open a tree the reader closed.
func (w *window) selectTreeFile(i int) {
	key := "f:" + w.files[i].Path
	if w.treeSel == key {
		return
	}
	w.treeSel = key
	for r, row := range w.visibleTree() {
		if row.key == key {
			w.treeList.ScrollIntoView(r)
			return
		}
	}
}

// historyView lists the commits of the History tab.
func (w *window) historyView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	q := strings.ToLower(strings.TrimSpace(w.historyFilter))
	type entry struct {
		local  bool
		commit *git.Commit
	}
	var entries []entry
	if q == "" {
		entries = append(entries, entry{local: true})
	}
	for i := range w.history {
		cm := &w.history[i]
		if q == "" || strings.Contains(strings.ToLower(cm.Subject), q) || strings.HasPrefix(cm.Hash, q) || strings.Contains(strings.ToLower(cm.Author), q) {
			entries = append(entries, entry{commit: cm})
		}
	}
	target := w.source
	current := -1
	for i, e := range entries {
		if (e.local && (target.kind == sourceWorkingTree || target.kind == sourceBranch)) || (e.commit != nil && target.kind == sourceCommit && target.ref == e.commit.Hash) {
			current = i
		}
	}
	open := func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		w.commitOpen = false
		w.historyList.ScrollIntoView(i)
		if entries[i].local {
			w.setSource(w.launchWorkTree())
		} else {
			w.setSource(source{kind: sourceCommit, ref: entries[i].commit.Hash})
		}
	}
	w.historyList.Key = func(i int) any {
		if entries[i].local {
			return "local"
		}
		return entries[i].commit.Hash
	}
	w.historyList.Label = func(i int) string {
		if entries[i].local {
			return msg().UncommittedChanges()
		}
		return entries[i].commit.Subject
	}
	now := time.Now()
	history := ui.List(c, &w.historyList, len(entries), func(i int) {
		focused := w.historyList.FocusWithin(c) || w.focusHistory
		e := entries[i]
		row := ui.Row(c).Gap(8).Padding(5, 8).Radius(6).AlignItems(ui.Start).Role(ui.RoleButton)
		if e.local {
			row.Label(msg().UncommittedChanges())
		} else {
			row.Label(e.commit.Subject)
		}
		muted, ref := t.TextMuted, pal.ref
		switch {
		case i == current && focused:
			row.Background(pal.selection)
		case i == current:
			row.Background(pal.surfaceHover)
		case row.Hovered():
			row.Background(pal.hover)
		}
		if (row.Pressed() || row.Clicked()) && i != current {
			if debugFrames {
				log.Printf("history row %d clicked", i)
			}
			open(i)
		}
		row.Children(func() {
			if e.local {
				ui.Text(c, "local").Font(w.codeFont()).FontSize(12).TextColor(ref).Width(56).Shrink(0)
				ui.Text(c, msg().UncommittedChanges()).FontSize(12).SingleLine().Grow(1)
				return
			}
			cm := e.commit
			when := w.commitTimes[cm.Hash]
			if when.at != now.Truncate(time.Minute) {
				// Formatted once a minute, rather than every frame.
				when = commitTime{at: now.Truncate(time.Minute), ago: relativeTime(now, cm.Time), full: cm.Time.Format("Mon Jan 2 15:04:05 2006")}
				w.commitTimes[cm.Hash] = when
			}
			ui.Text(c, cm.Short).Font(w.codeFont()).FontSize(12).TextColor(ref).Width(56).Shrink(0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, cm.Subject).FontSize(12).SingleLine().Tooltip(cm.Subject)
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, cm.Author).FontSize(10).SingleLine().TextColor(muted).Grow(1).MinWidth(0)
					ui.Text(c, when.ago).FontSize(10).TextColor(muted).Shrink(0).Tooltip(when.full)
				})
			})
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().Label(msg().HistoryLabel())
	focusRing(history, pal, 6)
	if w.focusHistory && w.historyList.Focus(c) {
		w.focusHistory = false
	}
	history.Children(func() {
		if len(entries) == 0 {
			emptyState(c, pal, emptyBox{shape: emptySmall, icon: iconHistory, title: textNoCommitsTab(), detail: q})
		}
	})
	if w.historyList.Shortcut(c, 0, ui.KeyDown) {
		open(current + 1)
	}
	if w.historyList.Shortcut(c, 0, ui.KeyUp) {
		open(max(current-1, 0))
	}
	// More commits load well before the end comes into view, so that
	// scrolling does not stop there.
	if q == "" && w.historyMore && !w.historyLoading {
		if _, last := w.historyList.Visible(); last >= len(entries)-historyPage/2 {
			w.historyLimit += historyPage
			w.loadHistory()
		}
	}
}

// commitTime is how a commit's time shows, as of a minute.
type commitTime struct {
	at        time.Time
	ago, full string
}

// launchWorkTree is the source of uncommitted changes: the work tree, or
// its comparison with a branch when the window was opened on one.
func (w *window) launchWorkTree() source {
	if w.launch.kind == sourceBranch {
		return w.launch
	}
	return source{kind: sourceWorkingTree}
}

// relativeTime writes how long ago t was: just now, 5m ago, 3d ago; ""
// for no time.
func relativeTime(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return msg().JustNow()
	case d < time.Hour:
		return msg().MinutesAgo(int(d / time.Minute))
	case d < 24*time.Hour:
		return msg().HoursAgo(int(d / time.Hour))
	case d < 30*24*time.Hour:
		return msg().DaysAgo(int(d / (24 * time.Hour)))
	case d < 365*24*time.Hour:
		return msg().MonthsAgo(int(d / (30 * 24 * time.Hour)))
	}
	return msg().YearsAgo(int(d / (365 * 24 * time.Hour)))
}

// sidebarFooter shows the total of the changes, and the Commit button.
func (w *window) sidebarFooter(c *ui.Context, pal *palette) {
	t := c.Theme()
	adds, dels, counted := 0, 0, false
	for i, f := range w.files {
		if w.fileVisible(i) && countable(f) {
			adds += f.Additions
			dels += f.Deletions
			counted = true
		}
	}
	canCommit := w.tab == 0 && w.source.kind == sourceWorkingTree && len(w.files) > 0
	if !(counted && w.tab == 0) && !canCommit {
		return
	}
	ui.Row(c).MinHeight(40).Padding(6, 10).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(pal.border).Children(func() {
		if counted && w.tab == 0 {
			ui.Row(c).Gap(6).Tooltip(msg().TotalChangeTip(adds, dels)).Children(func() {
				ui.Text(c, msg().TotalLabel()).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
				ui.Text(c, "+"+thousands(adds)).Font(w.codeFont()).FontSize(11).FontWeight(600).TextColor(pal.addText)
				ui.Text(c, "-"+thousands(dels)).Font(w.codeFont()).FontSize(11).FontWeight(600).TextColor(pal.delText)
			})
		}
		ui.Spacer(c)
		if canCommit {
			label := msg().CommitAction()
			if w.commitOpen {
				label = msg().ReviewAction()
			}
			b := ui.Button(c, "").Children(func() {
				ui.Icon(c, iconCommit).FontSize(14)
				ui.Text(c, label).SingleLine()
			})
			if b.Clicked() {
				w.toggleCommit()
			}
		}
	})
}

// sidebarResizer is the edge of the sidebar the user drags.
func (w *window) sidebarResizer(c *ui.Context, pal *palette) {
	ui.Box(c).Width(1).Shrink(0).Background(pal.border).Children(func() {
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-4).Width(9).Cursor(ui.CursorResizeEW)
		if dx, _, held := handle.Dragged(); held {
			w.dragWidth += dx
			switch {
			case w.dragWidth < sidebarCollapse:
				w.sidebarShown = false
			default:
				w.sidebarShown = true
				w.sidebarWidth = min(max(w.dragWidth, sidebarMin), sidebarMax)
			}
		} else {
			if w.dragWidth != 0 && w.dragWidth != w.sidebarWidth {
				go state.setLayout(w.sidebarWidth, w.sidebarShown)
			}
			w.dragWidth = w.sidebarWidth
		}
	})
}

// foldAllRow is the pair of buttons that fold the whole tree at once, or open
// it whole: one reading of the change, or its shape alone.
func (w *window) foldAllRow(c *ui.Context, pal *palette) {
	t := c.Theme()
	fold := func(want bool) func() {
		return func() { w.foldAll(want) }
	}
	ui.Row(c).Padding(0, 10, 4, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		groups := len(w.groups)
		if groups == 0 {
			ui.Text(c, msg().FilesCount(len(w.files))).FontSize(11).TextColor(t.TextMuted).Grow(1).SingleLine()
		} else {
			ui.Text(c, msg().GroupsFiles(groups, len(w.files))).FontSize(11).TextColor(t.TextMuted).
				Grow(1).Shrink(1).SingleLine()
		}
		for _, b := range []struct {
			label, tip string
			fold       bool
		}{
			{msg().FoldAll(), msg().FoldAllTip(), true},
			{msg().UnfoldAll(), msg().UnfoldAllTip(), false},
		} {
			b := b
			btn := ui.ButtonBase(c).Padding(2, 6).Radius(5).Label(b.tip).Tooltip(b.tip)
			if btn.Hovered() {
				btn.Background(pal.surfaceHover)
			}
			btn.Children(func() {
				ui.Text(c, b.label).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
			})
			if btn.Clicked() {
				fold(b.fold)()
			}
		}
	})
}

// foldAll folds every group and every folder of the tree, and the cards of
// every file where the surface stacks them; unfold opens them whole. The file
// read keeps its place, as a fold is no navigation.
//
// A fold of the whole tree is one click that loses where every group stood, so
// it keeps what it found and offers it back for a few seconds.
func (w *window) foldAll(fold bool) {
	before := w.folded()
	for key, n := range w.treeItems {
		if n != nil && n.dir {
			w.closedDirs[key] = fold
		}
	}
	if !w.oneFile() {
		file, chosen := w.current, w.treeSel
		for _, f := range w.files {
			f.collapsed = fold
		}
		w.current, w.treeSel = file, chosen
		w.revealedAt = w.now
	}
	w.rowsDirty = true
	w.offerUndo(foldedAll(fold, len(w.groups), len(w.files)), func() { w.refold(before) })
}

// foldedAll says what a fold of the whole tree took, for the bar of an undo:
// its groups, or its files where the rules group none.
func foldedAll(fold bool, groups, files int) string {
	switch {
	case fold && groups > 0:
		return msg().FoldedAllGroups(groups)
	case fold:
		return msg().FoldedAllFiles(files)
	case groups > 0:
		return msg().UnfoldedAllGroups(groups)
	}
	return msg().UnfoldedAllFiles(files)
}

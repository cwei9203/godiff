package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/highlight"
	"github.com/egoist/mygo/ui"
)

// codeFont is the family of the code.
func (w *window) codeFont() string {
	if w.settings.CodeFontFamily != "" {
		return w.settings.CodeFontFamily + ", monospace"
	}
	return "SF Mono, Menlo, monospace"
}

func (w *window) codeSize() float32 { return float32(w.settings.CodeFontSize) }

// lineHeight is the height of a line of code: 20 at 13.
func (w *window) lineHeight() float32 {
	return float32(math.Round(float64(w.codeSize()) * 20 / 13))
}

// charWidth is the width of a column of code.
func (w *window) charWidth(c *ui.Context) float32 {
	key := fmt.Sprint(w.codeFont(), w.codeSize())
	if w.charKey != key {
		width, _ := c.MeasureText(0, ui.Span{Text: strings.Repeat("0", 20), Font: w.codeFont(), Size: w.codeSize()})
		w.charW, w.charKey = width/20, key
	}
	return w.charW
}

// fileMetrics are the widths a file's rows share: of its line numbers,
// and how far its lines go.
type fileMetrics struct {
	digits  int
	maxCols int
}

func (f *fileState) metrics() fileMetrics {
	if f.metricsDone {
		return f.metric
	}
	maxNum, maxCols := 0, 0
	for _, h := range f.Hunks {
		maxNum = max(maxNum, h.OldStart+h.OldLines, h.NewStart+h.NewLines)
		for _, l := range h.Lines {
			maxCols = max(maxCols, columns(l.Text))
		}
	}
	maxNum = max(maxNum, len(f.oldLines), len(f.newLines))
	if f.canExpand() {
		for _, l := range f.newLines {
			maxCols = max(maxCols, columns(l))
		}
		for _, l := range f.oldLines {
			maxCols = max(maxCols, columns(l))
		}
	}
	f.metric = fileMetrics{digits: max(len(fmt.Sprint(maxNum)), 2), maxCols: maxCols}
	f.metricsDone = f.loaded
	return f.metric
}

// diffList shows the files' cards, building the rows in view only.
func (w *window) diffList(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	// A window opened again comes back to the file shown last, before the
	// rows are laid out for the file it would show first.
	w.restorePosition()
	shown := w.current
	if w.oneFile() {
		shown = w.shownFile()
		if shown != w.shownFor {
			// The file chosen changed, and the rows still show the one
			// before: as a match of the find bar or a note goes there.
			w.rowsDirty = true
		}
	}
	if w.rowsDirty {
		w.updateMatches()
		w.buildRows()
	}
	if w.oneFile() && shown != w.shownFor {
		// The file shown alone replaces the rows of the one before: they
		// start at its top, or at the match of the find bar in it.
		w.shownFor = shown
		if shown >= 0 && shown != w.current {
			// The file chosen left the surface, as a filter hides it: the
			// one that took its place is the one shown.
			w.current = shown
			w.selectTreeFile(shown)
		}
		if !w.matchRow() {
			w.list.ScrollTo(0, ui.Start)
		}
	}
	// The review is a place the keys go, as the tree and the drawer are, and
	// one stop of Tab rather than one for every button its cards hold: Tab
	// stops on the review, and the file-level actions have keys and menus of
	// their own.
	changes := ui.List(c, &w.list, len(w.rows), func(i int) { w.diffRow(c, pal, i, w.list.FocusWithin(c)) }).
		Grow(1).MinWidth(0).Padding(0, 12, 24).Background(pal.appBg).Focusable().FocusGroup(ui.Vertical).
		Label(msg().ChangesLabel())
	focusRing(changes, pal, 0)
	// The file shown has a menu of its own from the keys as well: Shift+F10 on
	// the review opens what its header opens on a right-click, so that the
	// actions of a card are not the pointer's alone.
	changes.ContextMenu(func(m *ui.Menu) {
		if w.current >= 0 && w.current < len(w.files) {
			w.fileMenu(c, m, w.files[w.current])
		}
	})

	// The file at the top follows the scrolling, and the tree with it.
	first, _ := w.list.Visible()
	for first >= 0 && first < len(w.rows) && w.rows[first].banner() {
		first++
	}
	if first >= 0 && first < len(w.rows) {
		file := int(w.rows[first].file)
		if file != w.current && time.Since(w.revealedAt) > 1200*time.Millisecond {
			w.current = file
			w.selectTreeFile(file)
		}
	}
	// The file shown is where the next window opens, once it has stayed a
	// while: a walk through the files saves the last alone.
	w.rememberPosition()
}

// matchRow scrolls the row of the match the find bar is on into the middle
// of the surface, and reports whether it found it.
func (w *window) matchRow() bool {
	if !w.searching() || w.match < 0 || w.match >= len(w.matches) {
		return false
	}
	m := w.matches[w.match]
	for i := range w.rows {
		r := &w.rows[i]
		if int(r.file) != m.file {
			continue
		}
		if m.hunk < 0 && r.kind == rowHeader {
			w.list.ScrollTo(i, ui.Start)
			return true
		}
		if r.kind == rowLine && r.hunk == m.hunk && (r.a == m.line || r.b == m.line) {
			w.list.ScrollTo(i, ui.Center)
			return true
		}
	}
	return false
}

// fileNavRow heads the file shown alone: the way to the file before it and
// the one after it, where it stands among the files, its name, and the
// group it belongs to.
func (w *window) fileNavRow(c *ui.Context, pal *palette, idx int) {
	t := c.Theme()
	f := w.files[idx]
	at, total := w.fileNav()
	ui.Row(c).Height(lineGroup).Padding(0, 6).Gap(8).Margin(gapBlock, 0, 0).Radius(radiusCard).
		Border(1, pal.border).Background(pal.surface).Children(func() {
		step := func(svg *ui.SVG, turn, dir int, tip string) {
			b := ui.ButtonBase(c).Size(26, 26).Radius(6).Center().Label(tip).Tooltip(tip).FocusRing(false).
				Disabled(!w.canStep(dir))
			if !w.canStep(dir) {
				b.Opacity(0.4)
			} else if b.Hovered() {
				b.Background(pal.surfaceHover)
			}
			b.Children(func() { uiIcon(c, svg, iconBase).TextColor(t.TextMuted).Rotate(float32(turn)) })
			if b.Clicked() {
				w.stepFile(dir)
			}
		}
		step(iconChevronRight, 180, -1, msg().PreviousFile())
		ui.Text(c, fmt.Sprintf("%d / %d", at, total)).Font(w.codeFont()).FontSize(12).FontWeight(600).
			TextColor(t.TextMuted).Shrink(0).Tooltip(msg().FilePosition(at, total))
		ui.Row(c).Gap(5).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			fileIcon(c, f, iconSmall)
			ui.RichText(c,
				ui.Span{Text: f.Dir(), Color: t.TextMuted},
				ui.Span{Text: f.Name(), Color: t.Text, Weight: 600},
			).Font(w.codeFont()).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
		})
		if f.group >= 0 && f.group < len(w.groups) {
			g := &w.groups[f.group]
			svg, accent := groupMark(g)
			ui.Row(c).Gap(5).Padding(3, 8).Radius(radiusChip).Background(pal.pill).Shrink(1).MinWidth(0).
				Label(msg().InGroup(g.label)).Tooltip(msg().GroupTip(g.label)).Children(func() {
				uiIcon(c, svg, iconSmall).TextColor(categoryColor(accent, t, pal)).Shrink(0)
				ui.Text(c, g.label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			})
		}
		step(iconChevronRight, 0, 1, msg().NextFile())
		if w.unviewedMiss {
			// The step found nothing: every file of the review is viewed.
			emptyState(c, pal, emptyBox{shape: emptyLine, icon: iconEye, title: textAllViewed(), detail: "",
				actions: func() {
					b := ui.ButtonBase(c).Padding(0, 4).Radius(4).Label(textResetReview())
					if b.Hovered() {
						b.Background(pal.hover)
					}
					b.Children(func() {
						ui.Text(c, textResetReview()).FontSize(11).FontWeight(600).TextColor(t.Accent)
					})
					if b.Clicked() {
						w.resetReviewState()
					}
				}})
		}
		// The same step for the reader who reviews by what is left: the first
		// file not viewed yet, round from this one.
		unviewed := ui.ButtonBase(c).Size(26, 26).Radius(6).Center().Label(msg().NextUnviewed()).
			Tooltip(msg().NextUnviewed()).FocusRing(false)
		if unviewed.Hovered() {
			unviewed.Background(pal.surfaceHover)
		}
		unviewed.Children(func() { uiIcon(c, iconEye, iconBase).TextColor(t.TextMuted) })
		if unviewed.Clicked() {
			w.stepUnviewed(1)
		}
	})
}

// revealFile scrolls the surface to a file's card.
func (w *window) revealFile(i int) {
	// The file gone to is the one shown, before the rows are laid out: the
	// file view lays out that file's rows alone.
	w.current = i
	if w.rowsDirty {
		w.buildRows()
	}
	for r := range w.rows {
		if w.rows[r].kind == rowHeader && int(w.rows[r].file) == i {
			w.list.ScrollTo(r, ui.Start)
			break
		}
	}
	w.revealedAt = time.Now()
}

func (w *window) diffRow(c *ui.Context, pal *palette, i int, focused bool) {
	r := &w.rows[i]
	switch r.kind {
	case rowCommit:
		w.commitMessage(c, pal).Margin(gapBlock, 0, 0)
		return
	case rowPull:
		w.pullHeader(c, pal).Margin(gapBlock, 0, 0)
		return
	case rowSummary:
		if w.summaryFolded() && !w.summaryOpen {
			// The card summarizes every change, not the part shown: it folds
			// to a line that opens it again.
			w.summaryLine(c, pal)
			return
		}
		w.summaryCard(c, pal).Margin(gapBlock, 0, 0)
		return
	case rowGroup:
		w.groupRow(c, pal, int(r.gap))
		return
	case rowFileNav:
		w.fileNavRow(c, pal, int(r.file))
		return
	}
	f := w.files[r.file]
	switch r.kind {
	case rowThread:
		w.threadRow(c, pal, f, r.thread)
	case rowAINote:
		w.noteRow(c, pal, r.note)
	case rowHeader:
		w.fileHeader(c, pal, int(r.file), f, focused)
	case rowNote:
		w.card(c, pal).Padding(10, 16).Background(pal.gapBg).Children(func() {
			ui.Text(c, w.fileNote(f)).FontSize(12).TextColor(pal.gapText)
		})
	case rowGap:
		w.gapRow(c, pal, f, r)
	case rowImage:
		w.imageRow(c, pal, f)
	case rowLine:
		w.lineRow(c, pal, f, r)
	case rowComment:
		w.commentRow(c, pal, f, r.comment)
	case rowEnd:
		if f.collapsed && !w.forceOpen(int(r.file)) {
			ui.Box(c).Height(0)
			return
		}
		ui.Box(c).Height(4).Background(pal.codeBg).Radius(0, 0, radiusCard, radiusCard)
	}
}

// summaryLine is the agent's review folded to one line, as the file view
// shows it: the card tells of every change, not of the file shown. The line
// opens the card again.
func (w *window) summaryLine(c *ui.Context, pal *palette) {
	t := c.Theme()
	st := w.analyses[w.source]
	what := ""
	switch {
	case st == nil:
	case st.running:
		what = msg().Reviewing()
	case st.err != "":
		what = msg().ReviewFailedSuffix()
	case st.result != nil:
		a := st.result
		by := a.Agent
		if ag, ok := agent.ByName(a.Agent); ok {
			by = ag.Label
		}
		if a.Model != "" {
			by += " · " + a.Model
		}
		if since := relativeTime(w.now, a.GeneratedAt); since != "" {
			by += " · " + since
		}
		what = " · " + by
	default:
		what = msg().NotRun()
	}
	b := ui.ButtonBase(c).Height(lineGroup).Padding(0, 12, 0, 10).Gap(8).Margin(gapBlock, 0, 0).Radius(radiusCard).
		Background(pal.surface).Border(1, t.Accent.Alpha(0.3)).Label(textAIReview() + what).
		Tooltip(msg().ReviewEveryChange())
	if b.Hovered() {
		b.Background(pal.surfaceHover)
	}
	if b.Clicked() {
		w.summaryOpen = true
	}
	b.Children(func() {
		uiIcon(c, iconSparkle, iconBase).TextColor(t.Accent).Shrink(0)
		ui.Text(c, textAIReview()).FontSize(13).FontWeight(600).SingleLine().Shrink(0)
		ui.Text(c, what).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		uiIcon(c, iconChevronRight, iconBase).TextColor(t.TextMuted).Shrink(0)
	})
}

// card is a row inside a file's card. The card holds no outline of its own:
// its ground, on the canvas, sets it apart, and the header of the file draws
// the one line it takes when the pointer is over it, or when it is the file
// shown.
func (w *window) card(c *ui.Context, pal *palette) ui.Element {
	return ui.Row(c).Background(pal.codeBg)
}

// forceOpen reports whether a collapsed file shows its lines anyway, as
// the files holding matches of the find bar do.
func (w *window) forceOpen(i int) bool {
	return w.searching() && w.fileMatches[i]
}

func (w *window) fileHeader(c *ui.Context, pal *palette, idx int, f *fileState, focused bool) {
	t := c.Theme()
	collapsed := f.collapsed && !w.forceOpen(idx)
	viewed := w.isViewed(f)
	// The band above the header spaces the cards, and hides the lines
	// scrolling under it while it is pinned.
	band := ui.Column(c).Background(pal.appBg).Padding(gapBlock, 0, 0, 0)
	var h ui.Element
	band.Children(func() {
		h = ui.Row(c).Height(lineHeader).Padding(0, 8, 0, 6).Gap(8).Background(pal.surface).
			BorderWidth(1, 1, 1, 1).BorderColor(ui.Transparent)
	})
	if collapsed {
		h.Radius(radiusCard)
	} else {
		h.Radius(radiusCard, radiusCard, 0, 0)
	}
	switch {
	case idx == w.current && focused:
		h.Border(1, t.Accent.Alpha(0.6))
	case idx == w.current, h.Hovered():
		// The file shown, and the one under the pointer, take the one line a
		// card draws: the rest stand apart by their ground alone.
		h.Border(1, pal.border)
	}
	h.ContextMenu(func(m *ui.Menu) { w.fileMenu(c, m, f) })
	h.Children(func() {
		if f.group >= 0 && f.group < len(w.groups) {
			// Where the file belongs, so that a card says it too, and the way
			// to the group's other files.
			g := &w.groups[f.group]
			svg, accent := groupMark(g)
			crumb := ui.ButtonBase(c).Height(22).Padding(0, 7, 0, 6).Gap(5).Radius(6).Shrink(0).
				Label(msg().GroupTip(g.label)).Tooltip(msg().GoToGroup(g.label))
			if crumb.Hovered() {
				crumb.Background(pal.hover)
			}
			if crumb.Clicked() {
				w.showGroup(f.group)
			}
			crumb.Children(func() {
				uiIcon(c, svg, iconSmall).TextColor(categoryColor(accent, t, pal)).Shrink(0)
				ui.Text(c, g.label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine().MaxWidth(120)
				uiIcon(c, iconChevronRight, iconSmall).TextColor(t.TextMuted).Shrink(0)
			})
		}
		toggle := ui.ButtonBase(c).Gap(8).Grow(1).Shrink(1).MinWidth(0).Height(lineHeader).Label(f.Path).Tooltip(f.Path)
		if toggle.Clicked() {
			f.collapsed = !collapsed
			if !f.collapsed && w.searching() {
				w.fileMatches[idx] = true
			}
			w.rowsDirty = true
		}
		toggle.FocusRing(false).Children(func() {
			chevron := ui.Box(c).Size(26, 26).Radius(13).Center().Shrink(0)
			if toggle.Hovered() {
				chevron.Background(pal.hover)
			}
			chevron.Children(func() {
				target := float32(0)
				if collapsed {
					target = -90
				}
				ic := uiIcon(c, iconChevronDown, iconBase).TextColor(t.TextMuted)
				ic.Rotate(ic.Animate("rot", target, 160*time.Millisecond))
			})
			pathColor := t.Text
			if collapsed || viewed {
				pathColor = t.TextMuted
			}
			fileIcon(c, f, iconBase)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
				ui.RichText(c,
					ui.Span{Text: f.Dir(), Color: t.TextMuted},
					ui.Span{Text: f.Name(), Color: pathColor, Weight: 600},
				).Font(w.codeFont()).FontSize(13).SingleLine()
				if f.OldPath != f.Path {
					ui.Text(c, msg().RenamedFrom(f.OldPath)).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).SingleLine()
				}
			})
		})
		// The copy button shows while the pointer is over the header; it
		// is always built, so that the elements after it keep their state.
		cp := iconButton(c, iconCopy, msg().CopyPath())
		if !h.Hovered() && !cp.FocusVisible() {
			cp.Opacity(0)
		}
		if cp.Clicked() {
			c.WriteClipboard(f.Path)
			c.Toast(msg().CopiedFile(f.Name()))
		}
		open := iconButton(c, iconOpen, msg().OpenInEditor()).Disabled(f.Status == diff.Deleted || f.Directory)
		if f.Status == diff.Deleted {
			open.Tooltip(msg().DeletedCannotOpen())
		}
		if open.Clicked() {
			w.openInEditor(f.Path, firstLine(f))
		}
		if countable(f) && (f.Additions > 0 || f.Deletions > 0) {
			ui.Row(c).Gap(8).Padding(4, 9).Radius(radiusChip).Background(pal.pill).Shrink(0).
				Tooltip(msg().ChangedLinesTip(f.Additions, f.Deletions)).Children(func() {
				ui.Text(c, "+"+thousands(f.Additions)).Font(w.codeFont()).FontSize(12).FontWeight(600).TextColor(pal.addText)
				ui.Text(c, "-"+thousands(f.Deletions)).Font(w.codeFont()).FontSize(12).FontWeight(600).TextColor(pal.delText)
			})
		}
		if f.Generated {
			ui.Text(c, msg().GeneratedBadge()).FontSize(11).FontWeight(600).Padding(4, 9).Radius(radiusChip).
				Background(pal.ref.Alpha(0.15)).TextColor(pal.ref).Shrink(0)
		}
		if n := w.openThreads(f.Path); n > 0 {
			ui.Row(c).Gap(4).Padding(4, 9).Radius(radiusChip).Background(pal.pill).TextColor(t.TextMuted).Shrink(0).
				Tooltip(msg().UnresolvedThreads(n)).Children(func() {
				uiIcon(c, iconComment, iconSmall)
				ui.Text(c, itoa(n)).Font(w.codeFont()).FontSize(12).FontWeight(600)
			})
		}
		w.viewedButton(c, pal, f, viewed)
	})
}

// viewedButton marks a file viewed, which collapses it.
func (w *window) viewedButton(c *ui.Context, pal *palette, f *fileState, viewed bool) {
	t := c.Theme()
	on := viewed
	b := ui.CheckboxBase(c, &on).Gap(7).Height(30).Padding(0, 11).Radius(radiusChip).Shrink(0).
		Border(1, ui.RGBA(127, 127, 127, 0.22)).Label(msg().Viewed())
	b.OnClick(func() { w.setViewed(f, on) })
	if b.Hovered() {
		b.Background(pal.surfaceHover)
	}
	if viewed {
		b.Border(1, pal.viewed.Alpha(0.44)).TextColor(pal.viewed)
	}
	b.Children(func() {
		box := ui.Box(c).Size(15, 15).Radius(4).Center()
		if viewed {
			box.Background(pal.viewed).Children(func() {
				uiIcon(c, iconCheck, iconSmall).TextColor(pal.codeBg)
			})
		} else {
			box.Border(1.5, ui.RGBA(127, 127, 127, 0.45))
		}
		ui.Text(c, msg().Viewed()).FontSize(12).FontWeight(600).TextColor(map[bool]ui.Color{true: pal.viewed, false: t.Text}[viewed])
	})
}

// setAllCollapsed collapses or expands every file.
func (w *window) setAllCollapsed(collapsed bool) {
	for _, f := range w.files {
		f.collapsed = collapsed
	}
	w.rowsDirty = true
}

// firstLine is the first changed line of a file, to open it there.
func firstLine(f *fileState) int {
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Kind == diff.Add {
				return l.New
			}
		}
		if h.NewStart > 0 {
			return h.NewStart
		}
	}
	return 0
}

// gutterWidth is the width of a column of line numbers.
func (w *window) gutterWidth(c *ui.Context, f *fileState) float32 {
	return float32(f.metrics().digits)*w.charWidth(c) + 18
}

func (w *window) gapRow(c *ui.Context, pal *palette, f *fileState, r *row) {
	t := c.Theme()
	gutter := w.gutterWidth(c, f)
	if !w.splitFile(f) && !f.oneSided() {
		gutter = 2*gutter - 4
	}
	first, last := r.gap == 0, int(r.gap) == len(f.Hunks)
	n := int(r.count)
	expand := func(top, bottom int) {
		if f.expanded == nil {
			f.expanded = map[int]gapShown{}
		}
		s := f.expanded[int(r.gap)]
		s.top += top
		s.bottom += bottom
		f.expanded[int(r.gap)] = s
		f.metricsDone = false
		w.rowsDirty = true
	}
	all := func() { expand(n, 0) }
	can := f.canExpand()
	w.card(c, pal).Height(30).Background(pal.gapBg).Children(func() {
		ui.Row(c).Width(gutter + 4).Height(30).Shrink(0).Children(func() {
			button := func(svg *ui.SVG, tip string, action func()) {
				b := ui.ButtonBase(c).Grow(1).Height(30).Center().Label(tip).Tooltip(tip).TextColor(pal.gapText).Disabled(!can)
				b.BorderWidth(0, 2, 0, 0).BorderColor(pal.codeBg)
				if b.Hovered() {
					b.TextColor(t.Text).Background(pal.hover)
				}
				b.Children(func() { uiIcon(c, svg, iconSmall) })
				if b.Clicked() {
					action()
				}
			}
			switch {
			case n <= expandStep && first:
				button(iconArrowUp, msg().ShowLinesAbove(), all)
			case n <= expandStep && last:
				button(iconArrowDown, msg().ShowLinesBelow(), all)
			case n <= expandStep:
				button(iconExpand, msg().ShowLines(), all)
			default:
				if !first {
					button(iconArrowDown, msg().ShowMoreLines(expandStep), func() { expand(expandStep, 0) })
				}
				if !last {
					button(iconArrowUp, msg().ShowMoreLines(expandStep), func() { expand(0, expandStep) })
				}
			}
		})
		label := ui.ButtonBase(c).Padding(0, 12).Height(30).FocusRing(false).Disabled(!can).Children(func() {
			text := msg().UnmodifiedLines(n)
			if n == 1 {
				text = "1 unmodified line"
			}
			ui.Text(c, text).FontSize(12).TextColor(pal.gapText)
		})
		if label.Hovered() && can {
			label.Underline()
		}
		if label.Clicked() {
			switch {
			case n <= expandStep:
				all()
			case first:
				expand(0, expandStep)
			case last:
				expand(expandStep, 0)
			default:
				expand(expandStep, expandStep)
			}
		}
		if n > expandStep {
			b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Disabled(!can).Children(func() {
				ui.Text(c, msg().ExpandLines()).FontSize(12).FontWeight(600).TextColor(pal.gapText)
			})
			if b.Hovered() {
				b.Background(pal.hover)
			}
			if b.Clicked() {
				all()
			}
		}
		if h := r.gap; int(h) < len(f.Hunks) && f.Hunks[h].Section != "" {
			ui.Text(c, f.Hunks[h].Section).Font(w.codeFont()).FontSize(12).TextColor(pal.gapText).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		}
	})
}

// lineSide is what one side of a line row shows.
type lineSide struct {
	present bool
	kind    diff.LineKind
	num     int
	text    string
	segs    []highlight.Seg
	words   []diff.Range
	hunk    int32
	index   int32 // the line's index in its hunk, -1 for expanded context
}

// sideOf returns a line of a row for the old or the new side.
func (w *window) sideOf(f *fileState, r *row, s side) lineSide {
	if r.hunk < 0 {
		// Expanded context.
		ls := lineSide{present: true, kind: diff.Context, hunk: -1, index: -1}
		if s == sideOld {
			ls.num = int(r.old)
			ls.text = f.contextText(r.old, r.new)
			if int(r.old) <= len(f.oldHL) {
				ls.segs = f.oldHL[r.old-1]
			} else if int(r.new) <= len(f.newHL) {
				ls.segs = f.newHL[r.new-1]
			}
		} else {
			ls.num = int(r.new)
			ls.text = f.contextText(r.old, r.new)
			if int(r.new) <= len(f.newHL) {
				ls.segs = f.newHL[r.new-1]
			}
		}
		return ls
	}
	idx := r.a
	if s == sideNew && w.splitFile(f) {
		idx = r.b
	}
	l := f.lineAt(r.hunk, idx)
	if l == nil {
		return lineSide{}
	}
	ls := lineSide{present: true, kind: l.Kind, text: l.Text, hunk: r.hunk, index: idx}
	if l.Kind == diff.Del {
		ls.num = l.Old
		if l.Old <= len(f.oldHL) {
			ls.segs = f.oldHL[l.Old-1]
		}
	} else {
		ls.num = l.New
		if s == sideOld && l.Kind == diff.Context {
			ls.num = l.Old
		}
		if l.New > 0 && l.New <= len(f.newHL) {
			ls.segs = f.newHL[l.New-1]
		}
	}
	ls.words = f.words[[2]int{int(r.hunk), int(idx)}]
	return ls
}

func (w *window) lineRow(c *ui.Context, pal *palette, f *fileState, r *row) {
	gutter := w.gutterWidth(c, f)
	lh := w.lineHeight()
	e := w.card(c, pal).AlignItems(ui.Stretch).MinHeight(lh)
	hs := w.hscroll[f.Path]
	if !w.settings.WordWrap {
		handle := ui.Local(e, "input-handle", func() ui.Handle { return ui.Handle{} })
		e.Bind(handle)
		charWidth := w.charWidth(c)
		e.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputScroll || math.Abs(float64(ev.DX)) <= math.Abs(float64(ev.DY)) {
				return false
			}
			b := handle.Bounds(c)
			code := b.W - 2*gutter - 40
			if w.splitFile(f) {
				code = b.W/2 - gutter - 30
			}
			limit := max(float32(f.metrics().maxCols)*charWidth-code, 0)
			w.hscroll[f.Path] = min(max(w.hscroll[f.Path]+ev.DX, 0), limit)
			w.invalidate()
			return true
		})
	}
	if !w.splitFile(f) {
		s := w.sideOf(f, r, sideNew)
		sd := sideNew
		if s.kind == diff.Del {
			sd = sideOld
		}
		e.Children(func() {
			w.lineCell(c, pal, f, r, s, sd, gutter, hs, !f.oneSided())
		})
		return
	}
	e.Children(func() {
		left := w.sideOf(f, r, sideOld)
		right := w.sideOf(f, r, sideNew)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			w.lineCell(c, pal, f, r, left, sideOld, gutter, hs, false)
		})
		ui.Box(c).Width(1).Shrink(0).Background(pal.border)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			w.lineCell(c, pal, f, r, right, sideNew, gutter, hs, false)
		})
	})
}

// lineCell shows a line: its numbers and its code.
func (w *window) lineCell(c *ui.Context, pal *palette, f *fileState, r *row, s lineSide, sd side, gutter, hs float32, unified bool) {
	lh := w.lineHeight()
	if !s.present {
		ui.Box(c).Grow(1).Background(pal.emptySide).MinHeight(lh)
		return
	}
	bg, gutterBg, bar, numColor := pal.codeBg, pal.codeBg, ui.Transparent, pal.lineNumber
	var wordColor ui.Color
	switch s.kind {
	case diff.Add:
		bg, gutterBg, bar, numColor, wordColor = pal.addBg, pal.addGutter, pal.addBar, pal.addBar, pal.addWord
	case diff.Del:
		bg, gutterBg, bar, numColor, wordColor = pal.delBg, pal.delGutter, pal.delBar, pal.delBar, pal.delWord
	}
	cell := ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Stretch).Background(bg)
	if w.isSelectedLine(f, s) {
		cell.Background(blend(bg, pal.selection))
		gutterBg = blend(gutterBg, pal.selection)
	} else if cell.Hovered() {
		cell.Background(blend(bg, pal.hover.Alpha(0.5)))
		gutterBg = blend(gutterBg, pal.hover.Alpha(0.5))
	}
	if cell.Clicked() {
		w.addComment(f.Path, sd, s.num, s.num)
	}
	cell.ContextMenu(func(m *ui.Menu) {
		if m.Item(msg().CommentOnLineNumber(s.num)).Chosen() {
			w.addComment(f.Path, sd, s.num, s.num)
		}
		if m.Item(msg().CopyLine()).Chosen() {
			c.WriteClipboard(s.text)
		}
		m.Separator()
		line := s.num
		if sd == sideOld {
			line = 0
		}
		if m.Item(msg().OpenInEditor()).Disabled(f.Status == diff.Deleted).Chosen() {
			w.openInEditor(f.Path, line)
		}
		if m.Item(msg().CopyPath()).Chosen() {
			c.WriteClipboard(f.Path)
		}
	})
	cell.Children(func() {
		num := func(n int, width float32) {
			txt := ""
			if n > 0 {
				txt = fmt.Sprint(n)
			}
			ui.Text(c, txt).Font(w.codeFont()).FontSize(w.codeSize()-1).FixedLineHeight(lh).TextColor(numColor).
				Width(width).TextAlign(ui.End).Padding(0, 8, 0, 0).Shrink(0)
		}
		hovered := cell.Hovered()
		ui.Row(c).Shrink(0).AlignItems(ui.Start).Background(gutterBg).Children(func() {
			ui.Box(c).Width(4).AlignSelf(ui.Stretch).Background(bar)
			if unified {
				old, new := 0, 0
				if r.hunk < 0 {
					old, new = int(r.old), int(r.new)
				} else if l := f.lineAt(r.hunk, s.index); l != nil {
					old, new = l.Old, l.New
				}
				num(old, gutter-4)
				num(new, gutter-4)
			} else {
				num(s.num, gutter-4)
			}
		})
		spans := w.lineSpans(f, s, sd, pal, wordColor)
		// The button commenting on the line, at the edge of the numbers
		// while the pointer is over the line; always built, so that the
		// elements after it keep their state.
		numbers := gutter
		if unified {
			numbers = 2*gutter - 4
		}
		plus := ui.ButtonBase(c).Absolute().Top((lh-18)/2).Left(numbers-9).Size(18, 18).Radius(5).Center().
			Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Label(msg().Comment()).Tooltip(msg().CommentTip()).FocusRing(false)
		if !hovered {
			plus.Opacity(0)
		}
		plus.Children(func() { uiIcon(c, iconPlus, iconSmall) })
		if plus.Clicked() {
			w.addComment(f.Path, sd, s.num, s.num)
		}
		// The margin keeps the clipped code clear of the line numbers.
		code := ui.Box(c).Grow(1).Basis(0).MinWidth(0).ClipX().Margin(0, 10)
		code.Children(func() {
			text := ui.RichText(c, spans...).Font(w.codeFont()).
				FontSize(w.codeSize()).FixedLineHeight(lh).TextColor(pal.code)
			if !w.settings.WordWrap {
				text.NoWrap().AlignSelf(ui.Start).Left(-hs)
			}
		})
	})
}

// lineSpans styles the code of a line: kept from frame to frame, apart from
// the lines holding matches of the find bar, whose marks move.
func (w *window) lineSpans(f *fileState, s lineSide, sd side, pal *palette, wordColor ui.Color) []ui.Span {
	var marks []mark
	for _, rg := range s.words {
		marks = append(marks, mark{Range: rg, color: wordColor})
	}
	if w.searching() {
		if found := findRanges(s.text, w.query); found != nil {
			active := w.activeMatch(f, s)
			for _, rg := range found {
				color := pal.match
				if active {
					color = pal.matchNow
				}
				marks = append(marks, mark{Range: rg, color: color})
			}
			return codeSpans(s.text, s.segs, marks, pal)
		}
	}
	key := spanKey{hunk: s.hunk, index: s.index, num: int32(s.num), side: sd, dark: pal == &darkPalette}
	if spans, ok := f.spans[key]; ok {
		return spans
	}
	spans := codeSpans(s.text, s.segs, marks, pal)
	if f.spans == nil {
		f.spans = map[spanKey][]ui.Span{}
	}
	f.spans[key] = spans
	return spans
}

// blend lays a translucent color over another.
func blend(base, over ui.Color) ui.Color { return over.Over(base) }

// commentRow shows a comment, under the line it is on.
func (w *window) commentRow(c *ui.Context, pal *palette, f *fileState, cm *comment) {
	t := c.Theme()
	w.card(c, pal).Padding(8, 16).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Avatar(c, w.userName(), nil).Size(28, 28).Shrink(0)
		box := ui.Column(c).Grow(1).MinWidth(0).Radius(12).Border(1, pal.border).Background(pal.surface).Clip()
		box.Children(func() {
			ui.Row(c).Height(32).Padding(0, 4, 0, 10).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
				ui.Text(c, w.userName()).FontSize(12).Bold()
				ui.Text(c, cm.label()).FontSize(11).TextColor(t.TextMuted)
				ui.Spacer(c)
				if iconButton(c, iconClose, msg().DeleteComment()).Size(24, 24).Clicked() {
					w.deleteComment(cm)
				}
			})
			area := ui.TextAreaBase(c, &cm.text).Placeholder(msg().WriteReviewComment()).MinHeight(64).Padding(8, 10).
				FontSize(13).Label(msg().ReviewCommentLabel())
			if cm.focus {
				area.Focus()
				cm.focus = false
			}
			if area.Shortcut(ui.Cmd, ui.KeyEnter) {
				if w.source.kind == sourcePull && w.pr.canWrite() {
					w.postComment(cm, true)
				} else {
					w.focusList = true
				}
			}
			if area.Shortcut(0, ui.KeyEscape) {
				if cm.pending() {
					w.discarding = cm
				} else {
					w.deleteComment(cm)
					w.focusList = true
				}
			}
			focused := area.Focused()
			if focused {
				w.typing = true
			}
			if cm.wasFocused && !focused && !cm.pending() {
				// An empty comment the user left goes.
				w.deleteComment(cm)
			}
			cm.wasFocused = focused
			if focused {
				box.Border(1, t.Accent.Alpha(0.6))
			}
			if w.source.kind == sourcePull {
				w.prCommentActions(c, pal, cm)
			}
		})
	})
}

// userName is the name of the git user, for comments, which loadUser
// reads as the window opens.
func (w *window) userName() string {
	if w.user == "" {
		return msg().GitUser()
	}
	return w.user
}

// imageRow shows a changed picture: the old one and the new one, side by
// side, on a checkerboard for their transparency.
func (w *window) imageRow(c *ui.Context, pal *palette, f *fileState) {
	t := c.Theme()
	pane := func(label string, img *ui.Bitmap, size int, bar ui.Color) {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(14).Gap(8).AlignItems(ui.Center).Children(func() {
			if img == nil {
				ui.Text(c, msg().NoImage(strings.ToLower(label))).FontSize(12).TextColor(t.TextMuted)
				return
			}
			iw, ih := img.Size()
			scale := min(float32(1), 320/float32(max(ih, 1)))
			ui.Box(c).Size(float32(iw)*scale, float32(ih)*scale).MaxWidthPercent(100).Radius(4).Border(1, pal.border).Clip().
				Stripes(ui.RGBA(127, 127, 127, 0.12), 6, 6, 45).Children(func() {
				ui.Image(c, img).Fill()
			})
			ui.Row(c).Gap(6).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(bar)
				ui.Textf(c, "%s · %d×%d · %s", label, iw, ih, formatBytes(size)).FontSize(11).TextColor(t.TextMuted)
			})
		})
	}
	w.card(c, pal).AlignItems(ui.Stretch).Children(func() {
		if f.Status != diff.Added && f.Status != diff.Untracked {
			pane(msg().OldImage(), f.oldImage, f.oldSize, pal.delBar)
		}
		if f.Status != diff.Deleted && f.Status != diff.Added && f.Status != diff.Untracked {
			ui.Box(c).Width(1).Background(pal.border)
		}
		if f.Status != diff.Deleted {
			pane(msg().NewImage(), f.newImage, f.newSize, pal.addBar)
		}
	})
}

// formatBytes writes a size: 512 B, 1.5 KiB, 12 MiB.
func formatBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v, unit := float64(n)/1024, "KiB"
	for _, u := range []string{"MiB", "GiB"} {
		if v < 1024 {
			break
		}
		v, unit = v/1024, u
	}
	if v < 10 {
		return fmt.Sprintf("%.1f %s", v, unit)
	}
	return fmt.Sprintf("%.0f %s", v, unit)
}

// fileMenu fills the menu of a file: the actions of its card, which its header
// opens on a right-click, and the review opens from the keys.
func (w *window) fileMenu(c *ui.Context, m *ui.Menu, f *fileState) {
	viewed := w.isViewed(f)
	if m.Item(msg().OpenInEditor()).Disabled(f.Status == diff.Deleted).Chosen() {
		w.openInEditor(f.Path, firstLine(f))
	}
	if m.Item(msg().CopyPath()).Chosen() {
		c.WriteClipboard(f.Path)
	}
	if m.Item(msg().Viewed()).Checked(viewed).Chosen() {
		w.setViewed(f, !viewed)
	}
	m.Separator()
	if m.Item(msg().CollapseAll()).Chosen() {
		w.setAllCollapsed(true)
	}
	if m.Item(msg().ExpandAll()).Chosen() {
		w.setAllCollapsed(false)
	}
}

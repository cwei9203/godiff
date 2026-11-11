package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

func (w *window) view(c *ui.Context) {
	if debugFrames {
		start := time.Now()
		defer func() {
			d := time.Since(start)
			if since := start.Sub(w.switchedAt); since < 3*time.Second {
				log.Printf("frame %v after the switch: view %v (files %d, rows %d, loading %v)", since, d, len(w.files), len(w.rows), w.loading)
			} else if d > 4*time.Millisecond {
				log.Printf("slow view: %v (rows %d, comments %d)", d, len(w.rows), len(w.comments))
			}
		}()
	}
	t := c.Theme()
	pal := paletteFor(t)
	w.now = c.Now()
	if w.titleLang != interfaceLanguage {
		// The title is the window's, not the frame's: it says the target in
		// the interface's language when the window draws again.
		w.titleLang = interfaceLanguage
		if w.win != nil {
			w.win.SetTitle(windowTitle(w))
		}
	}
	if w.regroupPending {
		w.regroupPending = false
		w.regroup()
	}
	// On macOS, the window shows the sidebar's material where nothing is
	// drawn. Elsewhere it would show black, as under the translucent edge
	// of the sidebar, so the sidebar's color is drawn there instead.
	c.Root().Background(w.sidebarBg(t))

	reviewBuilt := false
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		right := w.settings.SidebarPosition == "right"
		if w.sidebarShown && !right {
			w.sidebar(c)
			w.sidebarResizer(c, pal)
		}
		ui.Column(c).Grow(1).MinWidth(0).Background(pal.appBg).Children(func() {
			// The bar comes last in the tree, and so last for Tab: the keys
			// go from the tree to the review, to the drawer, and to the bar,
			// never around the review to reach its own toolbar.
			ui.Column(c).Grow(1).MinWidth(0).Padding(titleBarHeight, 0, 0, 0).Children(func() {
				reviewBuilt = w.mainArea(c, pal)
				// The drawer of a repair stays at the bottom of the review:
				// it hosts the agent's process, not a part of the diff.
				w.fixDrawer(c, pal)
			})
			w.undoBar(c, pal)
			w.toolbar(c, pal)
		})
		if w.sidebarShown && right {
			w.sidebarResizer(c, pal)
			w.sidebar(c)
		}
	})
	w.shortcuts(c, reviewBuilt)
	w.palette(c)
	w.fixDialog(c)
	w.sourceDialog(c)
	w.shortcutsHelp(c)
	w.reviewDialog(c)
	if w.discarding != nil {
		open := true
		if choice := ui.AlertDialog(c, &open, msg().DiscardCommentTitle(), msg().DiscardCommentDetail(), msg().Cancel(), msg().DiscardAction()); choice == 1 {
			w.deleteComment(w.discarding)
			w.discarding = nil
		} else if !open {
			w.discarding = nil
		}
	}
	if !w.focusedOnce && reviewBuilt {
		// The review takes the keys as the window opens.
		w.focusedOnce = true
		w.focusList = true
	}
	if w.focusList && w.list.Focus(c) {
		w.focusList = false
	}
}

// debugFrames logs the views that take long to build.
var debugFrames = os.Getenv("GODIFF_DEBUG") != ""

// The widths below which the toolbar drops a control: the comments first, the
// search next, so that the View menu and the sidebar's own switch are what a
// narrow window keeps, as the review's own layout matters most when the room is
// small.
const (
	toolbarComments = 940
	toolbarSearch   = 820
)

// toolbar is the bar along the top of the review, which drags the window: the
// repository reviewed and where the review stands, then the few controls that
// are not a choice kept in the settings.
//
// Four of them, and no more: the search, the comments, one View menu holding
// every switch of the surface, and the sidebar's switch. What the review shows
// is a choice of the settings, so it lives in the menu whose items say where
// each stands; the native menus hold the same items over the same settings.
func (w *window) toolbar(c *ui.Context, pal *palette) {
	t := c.Theme()
	bar := c.TitleBar()
	sidebarRight := w.sidebarShown && w.settings.SidebarPosition == "right"
	left := float32(14)
	if !w.sidebarShown || sidebarRight {
		left = bar.Left + 8
	}
	right := float32(12)
	if !sidebarRight {
		right += bar.Right
	}
	width, _ := c.Size()
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Height(titleBarHeight).Padding(0, right, 0, left).Gap(8).
		DragWindow().BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Background(pal.surface.Alpha(0.6)).Children(func() {
		// The repository: its name, and where it is, and the way to
		// another one: the folders of late, or one to pick.
		w.repoSwitcher(c, pal)
		if w.branch != "" && w.source.kind != sourcePull {
			chip(c, pal, iconBranch, w.branch, t.TextMuted).Tooltip(msg().BranchChip(w.branch))
		}
		switch w.source.kind {
		case sourceCommit:
			label := shortHash(w.source.ref)
			chip(c, pal, iconCommit, label, pal.ref).Font(w.codeFont()).Tooltip(w.source.ref)
		case sourceBranch:
			chip(c, pal, iconBranch, msg().LocalBranch(w.source.ref), pal.ref)
		case sourcePull:
			repo, n := w.source.pull()
			tip := repo.String()
			if w.pr != nil && w.pr.meta != nil {
				tip = w.pr.meta.Title
				chip(c, pal, iconPull, fmt.Sprintf("#%d", n), pal.ref).Tooltip(tip).Shrink(0)
				chip(c, pal, iconBranch, w.pr.meta.Base.Ref+" ← "+w.pr.meta.Head.Ref, t.TextMuted).Tooltip(w.pr.meta.Base.Label + " ← " + w.pr.meta.Head.Label)
			} else {
				chip(c, pal, iconPull, fmt.Sprintf("#%d", n), pal.ref).Tooltip(tip).Shrink(0)
			}
		}
		if w.loading && len(w.files) > 0 {
			ui.Spinner(c).Label(msg().Loading()).Size(14, 14)
		}
		ui.Spacer(c)
		if !w.commitOpen {
			if width >= toolbarSearch {
				if iconButton(c, iconSearch, textFind()+" (⌘F)").Clicked() {
					w.finding = !w.finding
				}
			}
			if width >= toolbarComments {
				w.commentsButton(c, pal)
			}
			if w.source.kind == sourcePull {
				w.reviewButton(c, pal)
			}
		}
		w.viewMenu(c, pal)
		if iconButton(c, iconGear, msg().SettingsMenuItem()+" (⌘,)").Clicked() {
			openSettings()
		}
		w.sidebarToggle(c)
	})
}

// commentsButton is the count of the review's comments, which copies them as
// Markdown.
func (w *window) commentsButton(c *ui.Context, pal *palette) {
	t := c.Theme()
	n := w.pendingComments()
	copied := time.Since(w.copiedAt) < 2*time.Second
	cb := ui.ButtonBase(c).Height(28).Padding(0, 8).Gap(5).Radius(7).TextColor(t.TextMuted).
		Label(msg().CopyComments()).Tooltip(msg().CopyCommentsTip()).Disabled(n == 0)
	if n == 0 {
		cb.Opacity(0.5)
	}
	if cb.Hovered() {
		cb.Background(pal.surfaceHover)
	}
	if cb.Clicked() {
		w.copyComments()
		c.After(2 * time.Second)
	}
	cb.Children(func() {
		if copied {
			ui.Icon(c, iconCheck).FontSize(15).TextColor(pal.viewed)
		} else {
			ui.Icon(c, iconComment).FontSize(15)
		}
		ui.Textf(c, "%d", n).Font(w.codeFont()).FontSize(11).FontWeight(700)
	})
}

// viewMenu is the one control the surface's own switches live in: the diff's
// layout, the file view, the theme, the wrapping, the whitespace, and how the
// files group. Every item shows where it stands, and writes the settings the
// native menus write too.
func (w *window) viewMenu(c *ui.Context, pal *palette) {
	t := c.Theme()
	s := w.settings
	set := w.applySetting
	b := ui.MenuButton(c, "", func(m *ui.Menu) {
		if m.Item(textSplit()).Checked(s.DiffStyle == "split").Chosen() {
			set(func(s *Settings) { s.DiffStyle = "split" })
		}
		if m.Item(textUnified()).Checked(s.DiffStyle == "unified").Chosen() {
			set(func(s *Settings) { s.DiffStyle = "unified" })
		}
		m.Separator()
		if m.Item(textOneFile()).Checked(s.FileView != fileViewStack).Chosen() {
			set(func(s *Settings) { s.FileView = fileViewOne })
		}
		if m.Item(textStack()).Checked(s.FileView == fileViewStack).Chosen() {
			set(func(s *Settings) { s.FileView = fileViewStack })
		}
		m.Separator()
		m.Submenu(textTheme(), func(m *ui.Menu) {
			if m.Item(textSystem()).Checked(s.Theme == "system").Chosen() {
				set(func(s *Settings) { s.Theme = "system" })
			}
			if m.Item(textLight()).Checked(s.Theme == "light").Chosen() {
				set(func(s *Settings) { s.Theme = "light" })
			}
			if m.Item(textDark()).Checked(s.Theme == "dark").Chosen() {
				set(func(s *Settings) { s.Theme = "dark" })
			}
		})
		if m.Item(textWrap()).Checked(s.WordWrap).Chosen() {
			set(func(s *Settings) { s.WordWrap = !s.WordWrap })
		}
		if m.Item(textSpace()).Checked(s.ShowWhitespace).Chosen() {
			set(func(s *Settings) { s.ShowWhitespace = !s.ShowWhitespace })
		}
		m.Separator()
		m.Submenu(textGroup(), func(m *ui.Menu) {
			modes := []struct {
				label string
				mode  groupMode
			}{
				{textNoGroup(), groupNone},
				{textByKind(), groupKind},
				{textByAI(), groupAI},
			}
			for _, it := range modes {
				if m.Item(it.label).Checked(w.grouping() == it.mode).Chosen() {
					w.chooseGrouping(it.mode)
				}
			}
		})
	}).Height(28).Padding(0, 8).Gap(5).Radius(7).Label(textView()).Tooltip(msg().ViewMenuTip())
	if b.Hovered() {
		b.Background(pal.surfaceHover)
	}
	b.Children(func() {
		ui.Icon(c, iconEye).FontSize(15).TextColor(t.TextMuted)
		ui.Text(c, textView()).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Shrink(0)
		ui.Icon(c, iconChevronDown).FontSize(10).TextColor(t.TextMuted).Shrink(0)
	})
}

// chip is a label in a pill, as the branch.
func chip(c *ui.Context, pal *palette, svg *ui.SVG, label string, color ui.Color) ui.Element {
	return ui.Row(c).Gap(5).Padding(3, 8).Radius(12).Background(ui.RGBA(127, 127, 127, 0.08)).
		Border(1, ui.RGBA(127, 127, 127, 0.12)).TextColor(color).Shrink(1).MinWidth(0).MaxWidth(240).Children(func() {
		ui.Icon(c, svg).FontSize(12)
		ui.Text(c, label).FontSize(12).FontWeight(600).SingleLine()
	})
}

// mainArea shows the review, the commit view, or why there is nothing,
// and reports whether it built the review list in this pass.
func (w *window) mainArea(c *ui.Context, pal *palette) bool {
	if w.commitOpen && w.source.kind == sourceWorkingTree {
		w.commitView(c)
		return false
	}
	if w.changed {
		ui.Row(c).Justify(ui.Center).Padding(8, 12, 0).Children(func() {
			ui.Row(c).Gap(6).Padding(5, 6, 5, 12).Radius(16).Background(pal.viewed.Alpha(0.1).Over(pal.codeBg)).
				Border(1, pal.viewed.Alpha(0.2)).Children(func() {
				what := msg().LocalChangesDetected()
				if w.source.kind == sourcePull {
					what = msg().NewCommitsPushed()
				}
				ui.Text(c, what).FontSize(13).FontWeight(600).TextColor(pal.viewed)
				ref := ui.ButtonBase(c).FocusRing(true).Children(func() {
					ui.Text(c, msg().RefreshToSeeThem()).FontSize(13).FontWeight(600).Underline().TextColor(pal.viewed)
				})
				if ref.Clicked() {
					w.refresh()
				}
				if iconButton(c, iconClose, msg().Dismiss()).Size(22, 22).Clicked() {
					w.changed = false
				}
			})
		})
	}
	// The groups show in the sidebar while it shows the files, and as chips
	// on the surface when it does not: one place for them, not both.
	groupsInSidebar := w.sidebarShown && w.tab == tabFiles
	if len(w.files) > 0 && w.loadErr == nil && !groupsInSidebar {
		w.groupChips(c, pal)
	}
	find := ui.FindBar(c, &w.finding, &w.query, len(w.matches), &w.match).Label(textFind())
	if find.FocusWithin() {
		w.typing = true
	}
	if find.Changed() {
		w.showMatch()
	}
	if w.query != w.matchesFor && (w.finding || w.matchesFor != "") {
		w.rowsDirty = true
	}
	if !w.finding && w.matchesFor != "" {
		w.query = ""
		w.rowsDirty = true
	}

	// A load shows "Thinking…" once it takes long: until then the window
	// keeps what it showed, so that quick loads change it once.
	waiting := w.loading && len(w.files) == 0
	slow := false
	if waiting {
		if left := thinkingDelay - time.Since(w.loadStart); left > 0 {
			c.After(left)
		} else {
			slow = true
		}
	}
	// The commit's message shows at once, from the history, until it
	// shows atop the changes.
	if w.source.kind == sourceCommit && w.commit != nil && w.loadErr == nil && len(w.files) == 0 {
		w.commitMessage(c, pal).Margin(gapBlock, 12, 0)
	}
	switch {
	case w.loadErr != nil && w.source.kind == sourcePull:
		emptyState(c, pal, emptyBox{title: msg().UnableToOpenPull(), detail: errorText(w.loadErr), mono: true,
			actions: func() {
				if ui.Button(c, msg().TryAgain()).Clicked() {
					w.load()
				}
			}})
	case w.loadErr != nil:
		emptyState(c, pal, emptyBox{title: msg().UnableToReadRepo(), detail: errorText(w.loadErr), mono: true})
	case slow:
		thinking(c, w.loadStatus)
	case waiting && len(w.files) == 0:
		ui.Box(c).Grow(1)
	case len(w.files) == 0:
		title, detail := textNoChanges(), abbreviateHome(w.repo.Root)
		switch w.source.kind {
		case sourceCommit:
			title, detail = textNoCommits(), shortHash(w.source.ref)
		case sourceBranch:
			title, detail = textNoBranch(), w.source.ref
		case sourcePull:
			title, detail = textNoPullFiles(), w.source.ref
		}
		emptyState(c, pal, emptyBox{title: title, detail: detail, mono: true, actions: func() {
			if w.source.kind == sourceWorkingTree && len(w.history) > 0 {
				if ui.Button(c, msg().ShowHistory()).Clicked() {
					w.tab, w.sidebarShown = tabHistory, true
				}
			}
		}})
	default:
		if w.rowsDirty {
			w.updateMatches()
			w.buildRows()
		}
		if len(w.rows) == 0 {
			title, detail := textNoMatchFiles(), strings.TrimSpace(w.filter)
			if w.searching() {
				title, detail = textNoMatchDiffs(), strings.TrimSpace(w.query)
			}
			if detail == "" {
				detail = msg().WhitespaceHidden()
			}
			emptyState(c, pal, emptyBox{title: title, detail: detail, mono: w.searching()})
			return false
		}
		w.diffList(c)
		return true
	}
	return false
}

// commitMessage shows the message of the commit reviewed.
func (w *window) commitMessage(c *ui.Context, pal *palette) ui.Element {
	t := c.Theme()
	cm := w.commit
	return ui.Column(c).Padding(padCard, 16).Gap(6).Radius(radiusCard).Background(pal.surface).Border(1, pal.border).Children(func() {
		ui.Row(c).Gap(10).Children(func() {
			ui.Avatar(c, cm.Author, nil).Size(26, 26)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, cm.Subject).FontSize(14).Bold().SingleLine().Selectable()
				ui.Text(c, msg().CommittedBy(cm.Author, relativeTime(w.now, cm.Time))).FontSize(11).TextColor(t.TextMuted).
					Tooltip(cm.Email + " · " + cm.Time.Format("Mon Jan 2 15:04:05 2006"))
			})
			ui.Text(c, shortHash(cm.Hash)).Font(w.codeFont()).FontSize(12).TextColor(pal.ref).Selectable()
		})
		if cm.Body != "" {
			ui.Text(c, cm.Body).FontSize(13).Selectable().Padding(4, 0, 0, 36).TextColor(t.Text)
		}
	})
}

// thinkingDelay is how long a load goes before the window says so.
const thinkingDelay = 200 * time.Millisecond

// thinking shows that the changes are loading, and what the load does
// when it says.
func thinking(c *ui.Context, status string) {
	t := c.Theme()
	if status == "" {
		status = msg().Thinking()
	}
	ui.Column(c).Grow(1).Center().Children(func() {
		label := ui.Text(c, status).Italic().FontSize(13).Font("SF Mono, Menlo, monospace").TextColor(t.TextMuted)
		label.Opacity(0.5 + 0.5*label.Loop("pulse", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut)))
	})
}

// shortcuts handles the window's keys that the menus do not.
func (w *window) shortcuts(c *ui.Context, reviewBuilt bool) {
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		w.paletteOpen = !w.paletteOpen
		w.paletteRow = 0
	}
	// Keys that type text belong to the fields: these work while no field
	// had the focus in the last frame.
	typing := w.typing
	w.typing = false
	if w.paletteOpen {
		return
	}
	// The bar of an undo goes on Escape, and only the bar: what would take
	// the key next keeps it.
	if w.undo != nil && !typing && c.Shortcut(0, ui.KeyEscape) {
		w.dismissUndo()
		return
	}
	if w.commitOpen || w.dialogOpen || w.help || typing || !reviewBuilt || (w.pr != nil && w.pr.submitOpen) {
		return
	}
	if c.Shortcut(0, ui.KeyJ) || c.Shortcut(ui.Ctrl, ui.KeyDown) {
		w.nextHunk(1)
	}
	if w.oneFile() && c.Shortcut(0, ui.KeyBracketLeft) {
		w.stepFile(-1)
	}
	if w.oneFile() && c.Shortcut(0, ui.KeyBracketRight) {
		w.stepFile(1)
	}
	if c.Shortcut(0, ui.KeyK) || c.Shortcut(ui.Ctrl, ui.KeyUp) {
		w.nextHunk(-1)
	}
	if w.list.Shortcut(c, 0, ui.KeyEnter) || (w.selHunk >= 0 && c.Shortcut(0, ui.KeyEnter)) {
		w.commentOnSelection()
	}
	// n and N step to the files not viewed yet, one file at a time: with
	// every file stacked they stay the notes', and the notes keep a key of
	// their own while the files are one at a time, ⌥n and ⌥⇧n.
	if w.oneFile() {
		if c.Shortcut(0, ui.KeyN) {
			w.stepUnviewed(1)
		}
		if c.Shortcut(ui.Shift, ui.KeyN) {
			w.stepUnviewed(-1)
		}
		if c.Shortcut(ui.Alt, ui.KeyN) {
			w.nextNote(1)
		}
		if c.Shortcut(ui.Alt|ui.Shift, ui.KeyN) {
			w.nextNote(-1)
		}
	} else {
		if c.Shortcut(0, ui.KeyN) {
			w.nextNote(1)
		}
		if c.Shortcut(ui.Shift, ui.KeyN) {
			w.nextNote(-1)
		}
	}
	if (w.selHunk >= 0 || w.noteSel != nil) && c.Shortcut(0, ui.KeyEscape) {
		w.selFile, w.selHunk, w.noteSel = -1, -1, nil
	}
	if c.Shortcut(ui.Alt, ui.KeyZ) {
		toggleWrap()
	}
	if c.Shortcut(ui.Shift, ui.KeySlash) {
		w.help = true
	}
}

// welcome is the window shown without a repository: it opens one, of the
// recent ones or of a folder.
type welcome struct {
	err error
	// list is the recent repositories, and choice the one the keys are on,
	// -1 for none.
	list    ui.ListState
	choice  int
	focused bool
}

// openFromWelcome opens a repository of the recent ones. It is a variable so
// that a test can watch the key that opens one without a window.
var openFromWelcome func(string)

func init() { openFromWelcome = openRecent }

func (v *welcome) view(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	c.Root().Background(pal.appBg)
	bar := c.TitleBar()
	ui.Box(c).Height(max(bar.Height, 40)).DragWindow().FillWidth()
	title, detail := msg().WelcomeTitle(), msg().WelcomeSubtitle()
	mono := false
	if v.err != nil && !errors.Is(v.err, git.ErrNotRepository) {
		title, detail, mono = msg().UnableToReadRepo(), errorText(v.err), true
	} else if v.err != nil {
		title, detail = textNoRepo(), textNoRepoDetail()
	}
	recent := state.recent()
	// The column stands at the top, 48 below the title bar, so that the
	// window reads as a page rather than as a panel floating in the middle.
	// Only a window too short for it scrolls.
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).AlignItems(ui.Center).Padding(48, 24, 32).Children(func() {
			ui.Column(c).Width(520).AlignItems(ui.Start).Gap(18).Children(func() {
				ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
					if appIcon != nil {
						ui.Image(c, appIcon).Size(44, 44).Radius(10)
					} else {
						ui.Icon(c, iconFileDiff).FontSize(36).TextColor(t.Accent)
					}
					ui.Column(c).Gap(2).Shrink(1).MinWidth(0).Children(func() {
						ui.Text(c, title).FontSize(20).FontWeight(600).SingleLine()
						description := ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted).MaxLines(2)
						if mono {
							description.Font("SF Mono, Menlo, monospace").Selectable().MaxLines(3)
						}
					})
				})
				// With no repository opened yet, the card holds the one
				// button: the empty state is a way in, not a line of text
				// beside one.
				if len(recent) == 0 {
					ui.Column(c).FillWidth().Padding(20).Gap(12).Radius(radiusCard).
						Background(pal.surface).Border(1, pal.border).Children(func() {
						ui.Text(c, msg().WelcomeEmpty()).FontSize(13).TextColor(t.TextMuted)
						open := ui.PrimaryButton(c, msg().OpenFolder()).Height(32).AlignSelf(ui.Start)
						if open.Clicked() {
							openFolder()
						}
						open.AutoFocus()
					})
				} else {
					open := ui.PrimaryButton(c, msg().OpenFolder()).Height(32)
					if open.Clicked() {
						openFolder()
					}
					ui.Text(c, msg().Recent()).FontSize(11).FontWeight(700).TextColor(t.TextMuted)
					v.list.Key = func(i int) any { return recent[i] }
					v.list.Label = func(i int) string { return filepath.Base(recent[i]) }
					ui.Column(c).FillWidth().Radius(radiusCard).Background(pal.surface).
						Border(1, pal.border).Clip().Children(func() {
						list := ui.List(c, &v.list, len(recent), func(i int) {
							v.row(c, pal, i, recent[i], i == len(recent)-1)
						}).Focusable().Label(msg().Recent())
						// The list takes the focus as the page opens, so the
						// ring waits for a key: a page is not a control until
						// the keys reach it.
						focusRingWhen(list, list.FocusVisible(), pal, radiusCard)
						if !v.focused && v.list.Focus(c) {
							v.focused = true
						}
						if v.list.Shortcut(c, 0, ui.KeyDown) {
							v.choice = min(v.choice+1, len(recent)-1)
							v.list.ScrollIntoView(v.choice)
						}
						if v.list.Shortcut(c, 0, ui.KeyUp) {
							v.choice = max(v.choice-1, 0)
							v.list.ScrollIntoView(v.choice)
						}
						if v.choice >= 0 && v.list.Shortcut(c, 0, ui.KeyEnter) {
							go openFromWelcome(recent[v.choice])
						}
					})
				}
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Text(c, msg().WelcomeRunBefore()).FontSize(11).TextColor(t.TextMuted)
					ui.Text(c, "godiff").Font("SF Mono, Menlo, monospace").FontSize(11).TextColor(t.TextMuted)
					ui.Text(c, msg().WelcomeRunAfter()).FontSize(11).TextColor(t.TextMuted)
				})
			})
		})
	})
}

// row is a repository of the recent ones: its name, where it is, and an
// arrow into it. last is the row the card's bottom edge closes.
func (v *welcome) row(c *ui.Context, pal *palette, i int, root string, last bool) {
	t := c.Theme()
	chosen := i == v.choice
	focused := v.list.FocusWithin(c)
	row := ui.Row(c).MinHeight(50).Padding(9, 14).Gap(10).MinWidth(0).Cursor(ui.CursorPointer).
		Label(filepath.Base(root) + " " + filepath.Dir(root))
	color, muted := t.Text, t.TextMuted
	if !last {
		row.BorderWidth(0, 0, 1, 0).BorderColor(pal.border)
	}
	switch {
	case chosen && focused:
		row.Background(pal.selection)
	case chosen:
		row.Background(pal.surfaceHover)
	case row.Hovered():
		row.Background(pal.surfaceHover)
	}
	if row.Clicked() {
		v.choice = i
		go openFromWelcome(root)
	}
	row.TextColor(color).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, filepath.Base(root)).FontSize(13).FontWeight(600).SingleLine()
			ui.Text(c, abbreviateHome(filepath.Dir(root))).FontSize(11).SingleLine().TextColor(muted).Tooltip(root)
		})
		ui.Icon(c, iconChevronRight).FontSize(13).TextColor(muted).Shrink(0)
	})
}

// repoSwitcher is the repository of the window: its name, which opens a panel
// of the repository's other targets and of the ways to another repository. A
// window is a session, so the panel reads the registry the Sessions tab reads,
// and acts on it through the same calls: there is no second list of sessions.
func (w *window) repoSwitcher(c *ui.Context, pal *palette) {
	t := c.Theme()
	root := w.repo.Root
	trigger := ui.ButtonBase(c).Height(38).Radius(7).Padding(0, 6).Label(textRepo()).Tooltip(msg().SwitchRepo())
	if w.repoMenuOpen {
		trigger.Background(pal.surfaceHover)
	} else if trigger.Hovered() {
		trigger.Background(pal.hover)
	}
	if trigger.Clicked() {
		w.repoMenuOpen = !w.repoMenuOpen
	}
	trigger.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Gap(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, sessionRepoName(w)).FontSize(13).Bold().SingleLine()
				ui.Text(c, filepath.Dir(abbreviateHome(root))).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
			ui.Icon(c, iconChevronDown).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		})
	})
	ui.PopoverBase(c, trigger, &w.repoMenuOpen, func(panel ui.Element) {
		panel.Width(272).Padding(6).Radius(radiusCard).Background(pal.surface).Border(1, pal.border).
			Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.26)).Clip().Label(textRepo())
		w.repoPanel(c, pal, panel)
	})
}

// repoPanel is the repository's panel, under its name in the title bar: where
// the repository is, the other targets it has a session for, and the ways to
// another repository or to none.
func (w *window) repoPanel(c *ui.Context, pal *palette, panel ui.Element) {
	t := c.Theme()
	// action is a row of the panel, as the menu items it stands in for.
	action := func(svg *ui.SVG, label string, off bool, run func()) {
		row := ui.ButtonBase(c).Height(28).Padding(0, 8).Gap(8).Radius(5).MinWidth(0).Label(label).Tooltip(label)
		if off {
			row.Disabled(true).Opacity(0.5)
		} else if row.Hovered() {
			row.Background(pal.hover)
		}
		if row.Clicked() && !off {
			w.repoMenuOpen = false
			run()
		}
		row.Children(func() {
			ui.Icon(c, svg).FontSize(13).TextColor(t.TextMuted)
			ui.Text(c, label).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		})
	}
	panel.Children(func() {
		ui.Text(c, abbreviateHome(w.repo.Root)).FontSize(11).TextColor(t.TextMuted).
			Padding(4, 8).SingleLine().Tooltip(w.repo.Root)
		// The other targets of this repository are the sessions the Sessions
		// tab lists under its name.
		for _, e := range repoSessions(w.repo.Root) {
			if e.w == w {
				continue
			}
			row := ui.ButtonBase(c).Height(28).Padding(0, 8).Gap(8).Radius(5).MinWidth(0).
				Label(e.target).Tooltip(msg().SessionGoTo(e.target))
			if row.Hovered() {
				row.Background(pal.hover)
			}
			if row.Clicked() {
				w.repoMenuOpen = false
				focusSession(e.w)
			}
			row.Children(func() {
				ui.Icon(c, iconLayers).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, e.target).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			})
		}
		ui.Divider(c).Margin(4, 0)
		action(iconFolderOpen, msg().OpenFolder(), false, openFolder)
		action(iconOpen, msg().RevealInFinder(), runtime.GOOS != "darwin", func() { revealInFinder(w.repo.Root) })
		action(iconClose, msg().CloseRepository(), false, func() { closeRepository(w) })
	})
}

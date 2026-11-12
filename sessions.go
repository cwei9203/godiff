package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
)

// A session is a window: one repository, and one target of it reviewed there.
// The registry of the windows is the list of the sessions, so nothing here
// keeps one of its own: the Sessions tab, the repository panel of the title
// bar, and openWindow all read the registry and act on it through
// focusSession and closeSession, which is what keeps the three from deciding
// differently what is open.

// sessionEntry is one session as the Sessions tab and the repository panel
// show it: the window, its repository, the target it reviews, and how far the
// review has come.
type sessionEntry struct {
	w      *window
	root   string
	target string
	// viewed is how many of the review's files were marked viewed, of total,
	// 0 and 0 while the review loads.
	viewed, total int
}

// sessionGroup is the sessions of one repository, in the order they opened.
type sessionGroup struct {
	root    string
	name    string
	entries []sessionEntry
}

// sessionRepoName names a repository as a list shows it: owner/repo where a
// remote of it on GitHub says so, else the folder's name.
func sessionRepoName(w *window) string {
	if w.gh.ok() {
		return w.gh.repo.String()
	}
	return filepath.Base(w.repo.Root)
}

// sessionTarget names the target a session reviews.
func sessionTarget(src source) string {
	switch src.kind {
	case sourceBranch:
		return msg().SessionBranch(src.ref)
	case sourceCommit:
		return msg().SessionCommit(shortHash(src.ref))
	case sourcePull:
		_, n := src.pull()
		return msg().SessionPull(n)
	}
	return msg().SessionWorktree()
}

// sessionProgress counts the files of a review that were marked viewed.
func sessionProgress(w *window) (viewed, total int) {
	for _, f := range w.files {
		if w.isViewed(f) {
			viewed++
		}
	}
	return viewed, len(w.files)
}

// listSessions reads the registry: the sessions of each repository, the
// repositories in the order they were opened.
func listSessions() []sessionGroup {
	var groups []sessionGroup
	for _, w := range sessionWindows() {
		at := slices.IndexFunc(groups, func(g sessionGroup) bool { return g.root == w.repo.Root })
		if at < 0 {
			groups = append(groups, sessionGroup{root: w.repo.Root, name: sessionRepoName(w)})
			at = len(groups) - 1
		}
		viewed, total := sessionProgress(w)
		groups[at].entries = append(groups[at].entries, sessionEntry{
			w: w, root: w.repo.Root, target: sessionTarget(w.source), viewed: viewed, total: total,
		})
	}
	return groups
}

// closeSession closes the window of a session, and opens the start page when
// it was the last one: with the windows gone, the app stands where a launch
// without a repository does, rather than on nothing.
func closeSession(w *window) {
	go func() {
		if w.win != nil {
			// The window's own close takes it out of the registry.
			w.win.Close()
		} else {
			// A window a test opened has none of the platform's to close.
			unregisterWindow(w)
		}
		if noWindows() {
			startPage(nil)
		}
	}()
}

// sessionRow is a row of the Sessions tab: a repository's name, or one of its
// sessions.
type sessionRow struct {
	group int
	entry int // -1 for the repository's own row
}

// sessionsView is the sidebar's Sessions tab: the sessions open, by
// repository, then the two ways to another one. A row brings its window to the
// front, which is what opening a session means: the windows are the sessions,
// so the tab is a way between them, never a way to a second copy of one.
func (w *window) sessionsView(c *ui.Context) {
	pal := paletteFor(c.Theme())
	t := c.Theme()
	groups := listSessions()
	var rows []sessionRow
	entries := 0
	for gi, g := range groups {
		rows = append(rows, sessionRow{group: gi, entry: -1})
		for ei := range g.entries {
			rows = append(rows, sessionRow{group: gi, entry: ei})
			entries++
		}
	}
	if w.sessionsSel >= len(rows) {
		w.sessionsSel = len(rows) - 1
	}
	if w.sessionsSel < 0 && len(rows) > 0 {
		w.sessionsSel = 0
	}
	choose := func(i int) {
		if i < 0 || i >= len(rows) || rows[i].entry < 0 {
			return
		}
		focusSession(groups[rows[i].group].entries[rows[i].entry].w)
	}
	w.sessionsList.Key = func(i int) any {
		if rows[i].entry < 0 {
			return "g:" + groups[rows[i].group].root
		}
		// The window itself is what a row stands for, and what tells the
		// rows apart: two sessions may review a target of the same name.
		return fmt.Sprintf("w:%p", groups[rows[i].group].entries[rows[i].entry].w)
	}
	w.sessionsList.Label = func(i int) string {
		if rows[i].entry < 0 {
			return groups[rows[i].group].name
		}
		return groups[rows[i].group].entries[rows[i].entry].target
	}
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		list := ui.List(c, &w.sessionsList, len(rows), func(i int) {
			r := rows[i]
			if r.entry < 0 {
				sessionGroupRow(c, pal, groups[r.group])
				return
			}
			w.sessionEntryRow(c, pal, groups[r.group].entries[r.entry])
		}).Grow(1).Padding(2, 8).Gap(1).Focusable().Label(msg().SessionsLabel())
		focusRing(list, pal, 6)
		if at := w.sessionsSel; at >= 0 {
			if w.sessionsList.Shortcut(c, 0, ui.KeyDown) {
				w.sessionsSel = min(at+1, len(rows)-1)
				w.sessionsList.ScrollIntoView(w.sessionsSel)
			}
			if w.sessionsList.Shortcut(c, 0, ui.KeyUp) {
				w.sessionsSel = max(at-1, 0)
				w.sessionsList.ScrollIntoView(w.sessionsSel)
			}
			if w.sessionsList.Shortcut(c, 0, ui.KeyEnter) {
				choose(w.sessionsSel)
			}
		}
		// One session alone is nothing to move between: the tab says how to
		// make another one, in the place the others would be listed.
		if entries == 1 {
			ui.Text(c, msg().SessionsEmpty()).FontSize(11).TextColor(t.TextMuted).Padding(6, 8).MaxLines(3)
		}
		w.sessionsFooter(c, pal)
	})
}

// sessionGroupRow is a repository's name atop its sessions.
func sessionGroupRow(c *ui.Context, pal *palette, g sessionGroup) {
	t := c.Theme()
	ui.Row(c).MinHeight(lineFile).Padding(2, 6).Gap(6).AlignItems(ui.Center).Children(func() {
		uiIcon(c, iconFolder, iconSmall).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, g.name).FontSize(12).FontWeight(600).TextColor(t.TextMuted).SingleLine().
			Grow(1).Shrink(1).MinWidth(0).Tooltip(abbreviateHome(g.root))
	})
}

// sessionEntryRow is one session: its target, how far its review has come, and
// the way to close it. The session the window is in says so with a dot rather
// than with a ground of the accent, which stays for what can be pressed.
func (w *window) sessionEntryRow(c *ui.Context, pal *palette, e sessionEntry) {
	t := c.Theme()
	current := e.w == w
	row := ui.Row(c).Height(lineFile+4).Padding(0, 4, 0, 6).Gap(6).Radius(6).MinWidth(0).
		Label(e.target).Tooltip(e.target)
	if row.Hovered() {
		row.Background(pal.hover)
	}
	if row.Clicked() {
		focusSession(e.w)
	}
	row.Children(func() {
		dot := ui.Box(c).Size(6, 6).Radius(3).Shrink(0)
		if current {
			dot.Background(t.Accent).Label(msg().SessionCurrent()).Tooltip(msg().SessionCurrent())
		}
		name := ui.Text(c, e.target).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		if current {
			name.FontWeight(600)
		}
		if e.total > 0 {
			count := ui.Textf(c, "%d/%d", e.viewed, e.total).Font(w.codeFont()).FontSize(10).
				FontWeight(600).TextColor(t.TextMuted).Shrink(0).Tooltip(msg().ViewedFilesTip())
			if e.viewed == e.total {
				count.TextColor(pal.viewed)
			}
		}
		close := ui.ButtonBase(c).Size(16, 16).Radius(4).Center().Shrink(0).
			Label(msg().SessionCloseTip()).Tooltip(msg().SessionCloseTip()).FocusRing(false)
		if close.Hovered() {
			close.Background(pal.surfaceHover)
		}
		close.Children(func() { uiIcon(c, iconClose, iconSmall).TextColor(t.TextMuted) })
		if close.Clicked() {
			closeSession(e.w)
		}
	})
}

// sessionsFooter is the two ways to another session, fixed under the list: a
// folder to pick, and the repositories opened lately, which show where they
// are asked for.
func (w *window) sessionsFooter(c *ui.Context, pal *palette) {
	t := c.Theme()
	ui.Column(c).Shrink(0).Padding(6, 8, 6, 8).Gap(1).BorderWidth(1, 0, 0, 0).
		BorderColor(pal.border).Children(func() {
		if w.recentOpen {
			for _, root := range state.recent() {
				sessionRecentRow(c, pal, root)
			}
		}
		open := ui.Row(c).Height(28).Padding(0, 6).Gap(7).Radius(6).MinWidth(0).
			Label(msg().OpenFolder()).Tooltip(msg().OpenRepoTitle())
		if open.Hovered() {
			open.Background(pal.hover)
		}
		if open.Clicked() {
			openFolder()
		}
		open.Children(func() {
			uiIcon(c, iconFolderOpen, iconSmall).TextColor(t.TextMuted)
			ui.Text(c, msg().OpenFolder()).FontSize(12).Grow(1).Shrink(1).MinWidth(0)
		})
		recent := ui.Row(c).Height(28).Padding(0, 6).Gap(7).Radius(6).MinWidth(0).
			Label(msg().Recent()).Tooltip(msg().SessionsRecentTip())
		if recent.Hovered() {
			recent.Background(pal.hover)
		}
		if recent.Clicked() {
			w.recentOpen = !w.recentOpen
		}
		recent.Children(func() {
			uiIcon(c, iconHistory, iconSmall).TextColor(t.TextMuted)
			ui.Text(c, msg().Recent()).FontSize(12).Grow(1).Shrink(1).MinWidth(0)
			arrow := uiIcon(c, iconChevronDown, iconSmall).TextColor(t.TextMuted).Shrink(0)
			turned := float32(0)
			if w.recentOpen {
				turned = -90
			}
			arrow.Rotate(arrow.Animate("recent", turned, 150*time.Millisecond))
		})
	})
}

// sessionRecentRow is a repository opened lately, which the Sessions tab opens
// as File → Open Recent does.
func sessionRecentRow(c *ui.Context, pal *palette, root string) {
	t := c.Theme()
	row := ui.Row(c).Height(26).Padding(0, 6, 0, 24).Gap(6).Radius(6).MinWidth(0).
		Label(filepath.Base(root)).Tooltip(root)
	if row.Hovered() {
		row.Background(pal.hover)
	}
	if row.Clicked() {
		go openRecent(root)
	}
	row.Children(func() {
		ui.Text(c, filepath.Base(root)).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		ui.Text(c, abbreviateHome(filepath.Dir(root))).FontSize(10).TextColor(t.TextMuted).
			SingleLine().Shrink(1).MinWidth(0).MaxWidth(130)
	})
}

// repoSessions lists the sessions of one repository: what the repository panel
// of the title bar shows the others of the window's own.
func repoSessions(root string) []sessionEntry {
	for _, g := range listSessions() {
		if g.root == root {
			return g.entries
		}
	}
	return nil
}

// savedSession is a session as the state keeps it: the repository, and the
// target reviewed there. A state file outlives the code that wrote it, so the
// kind of target is kept by its name rather than by its number.
type savedSession struct {
	Root string `json:"root"`
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}

// ofSession is the session of a window as the state keeps it.
func ofSession(w *window) savedSession {
	return savedSession{Root: w.repo.Root, Kind: w.source.kind.String(), Ref: w.source.ref}
}

// source reads the target a saved session reviewed, false for a kind it does
// not know: a state written by another version restores what it can.
func (s savedSession) source() (source, bool) {
	kind, ok := parseSourceKind(s.Kind)
	if !ok {
		return source{}, false
	}
	return source{kind: kind, ref: s.Ref}, true
}

// sessions returns the sessions the last run left open, in the order they
// opened.
func (s *store) sessions() []savedSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.Sessions)
}

// setSessions keeps the sessions open now, for the launch after this one.
func (s *store) setSessions(list []savedSession) {
	s.mu.Lock()
	if slices.Equal(s.data.Sessions, list) {
		s.mu.Unlock()
		return
	}
	s.data.Sessions = list
	s.mu.Unlock()
	s.save()
}

// openSessions is the sessions open now, in the order they opened.
func openSessions() []savedSession {
	var kept []savedSession
	for _, w := range sessionWindows() {
		kept = append(kept, ofSession(w))
	}
	return kept
}

// noteSessions writes the sessions open to the state, for a launch that
// restores them, as a window closes. Off, nothing is written and the next
// launch opens the page that opens a repository.
func noteSessions() {
	if !cfg.Get().RestoreSessions {
		return
	}
	state.setSessions(openSessions())
}

// restoreSessions reopens the sessions the last run left open, in the order
// they opened. One that cannot be is skipped without a word: a repository
// deleted, a commit gone, a branch that is no more. A launch that restores
// none of them opens the page that opens a repository.
func restoreSessions() {
	restored := 0
	for _, s := range state.sessions() {
		src, ok := s.source()
		if !ok {
			continue
		}
		if err := openWindow(s.Root, src); err != nil {
			continue
		}
		restored++
	}
	if restored == 0 {
		startPage(nil)
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// noPlatformWindows makes the sessions of a test ones without a window of the
// platform: MyGo's need a main loop the tests have not, and the registry and
// its rows are what the tests read.
func noPlatformWindows(t *testing.T) {
	t.Helper()
	old := newPlatformWindow
	newPlatformWindow = func(mygo.WindowOptions) *mygo.Window { return nil }
	t.Cleanup(func() { newPlatformWindow = old })
}

// openTestSession opens a window on a repository as the app opens one, without
// a window of the platform (newPlatformWindow answers nil), and takes it out of
// the registry when the test ends.
func openTestSession(t *testing.T, dir string, src source) (*window, *ui.Tester) {
	t.Helper()
	if err := openWindow(dir, src); err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	open := sessionWindows()
	w := open[len(open)-1]
	t.Cleanup(func() { unregisterWindow(w) })
	w.tab = tabSessions
	tt := ui.NewTester(w.view, 1280, 860)
	tt.Frame()
	return w, tt
}

// watchingFocus records the session a click goes to: a test's window has none
// of the platform's to take the focus, so the call that would focus it answers
// here instead.
func watchingFocus(t *testing.T) *[]*window {
	t.Helper()
	var went []*window
	old := focusSession
	focusSession = func(w *window) { went = append(went, w) }
	t.Cleanup(func() { focusSession = old })
	return &went
}

// watchingStartPage records the way back to the page a launch without a
// repository opens.
func watchingStartPage(t *testing.T) *int {
	t.Helper()
	showed := 0
	old := startPage
	startPage = func(error) { showed++ }
	t.Cleanup(func() { startPage = old })
	return &showed
}

// markViewed marks the first n files of a review viewed, as the reader does.
func markViewed(w *window, n int) {
	for i, f := range w.files {
		if i >= n {
			return
		}
		w.viewed[f.Path] = f.Fingerprint
	}
}

// closeBox clicks the × at the end of a row of the Sessions tab.
func closeBox(t *testing.T, tt *ui.Tester, row string) {
	t.Helper()
	box, ok := tt.Find(row)
	if !ok {
		t.Fatalf("no %q in %q", row, tt.Texts())
	}
	tt.ClickAt(box.X+box.W-8, box.Y+box.H/2)
	tt.Frame()
	tt.Frame()
}

// TestSessionsListsRepositories checks the Sessions tab over two repositories:
// each under its own name, with the target reviewed and how far its review has
// come.
func TestSessionsListsRepositories(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	first, second := testRepo(t), testRepo(t)
	gitIn(t, second, "branch", "feature")
	a, tt := openTestSession(t, first, source{})
	b, _ := openTestSession(t, second, source{kind: sourceBranch, ref: "feature"})
	markViewed(a, 1)
	markViewed(b, 2)
	a.tab = tabSessions
	tt.Frame()

	for _, want := range []string{
		sessionRepoName(a), sessionRepoName(b), "Working tree", "Branch feature", "1/4", "2/4",
	} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the sessions: %q", want, tt.Texts())
		}
	}
	if tt.HasText(msg().SessionsEmpty()) {
		t.Errorf("the empty state shows with two sessions: %q", tt.Texts())
	}
	if n := len(sessionWindows()); n != 2 {
		t.Errorf("%d sessions open, want the two", n)
	}
}

// TestSessionsEmptyState checks the hint of a tab with one session alone: the
// windows are the sessions, so it says how to make another, not that it has
// none.
func TestSessionsEmptyState(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	a, tt := openTestSession(t, testRepo(t), source{})
	a.tab = tabSessions
	tt.Frame()
	if !tt.HasText("Working tree") {
		t.Fatalf("the session is not listed: %q", tt.Texts())
	}
	if !tt.HasText("No other session open") {
		t.Errorf("the empty state is missing: %q", tt.Texts())
	}
}

// TestSessionsRowFocusesTheWindow checks that a row is a way to its window, not
// a way to a second one: the click focuses the window that is open.
func TestSessionsRowFocusesTheWindow(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	went := watchingFocus(t)
	first, second := testRepo(t), testRepo(t)
	gitIn(t, second, "branch", "feature")
	a, tt := openTestSession(t, first, source{})
	b, _ := openTestSession(t, second, source{kind: sourceBranch, ref: "feature"})
	a.tab = tabSessions
	tt.Frame()

	if err := tt.Click("Branch feature"); err != nil {
		t.Fatal(err)
	}
	if len(*went) != 1 || (*went)[0] != b {
		t.Fatalf("the click focused %d sessions, want the branch's one", len(*went))
	}
	if n := len(sessionWindows()); n != 2 {
		t.Errorf("%d sessions open after the click, want the two", n)
	}
}

// TestSessionsClose checks the × of a row: it closes that session alone, and
// the last one closed leads back to the page a launch without a repository
// opens.
func TestSessionsClose(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	showed := watchingStartPage(t)
	went := watchingFocus(t)
	first, second := testRepo(t), testRepo(t)
	gitIn(t, second, "branch", "feature")
	a, tt := openTestSession(t, first, source{})
	b, _ := openTestSession(t, second, source{kind: sourceBranch, ref: "feature"})
	a.tab = tabSessions
	tt.Frame()

	closeBox(t, tt, "Branch feature")
	if open := sessionWindows(); len(open) != 1 || open[0] != a {
		t.Fatalf("closing the branch left %d sessions, want the working tree's", len(open))
	}
	if tt.HasText("Branch feature") {
		t.Errorf("the closed session is still listed: %q", tt.Texts())
	}
	// The × closes a session; it does not go to it.
	if len(*went) != 0 {
		t.Errorf("closing the branch focused %d sessions, want none", len(*went))
	}
	if *showed != 0 {
		t.Error("the start page showed with a session left")
	}

	closeBox(t, tt, "Working tree")
	if open := sessionWindows(); len(open) != 0 {
		t.Fatalf("%d sessions open, want none", len(open))
	}
	if *showed != 1 {
		t.Errorf("the start page showed %d times after the last session closed, want once", *showed)
	}
	_ = b
}

// TestOpeningASessionTwice checks that a repository and a target already open
// come to the front: the sessions do not change, and no window is made.
func TestOpeningASessionTwice(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	went := watchingFocus(t)
	dir := testRepo(t)
	if err := openWindow(dir, source{}); err != nil {
		t.Fatal(err)
	}
	open := sessionWindows()
	if len(open) != 1 {
		t.Fatalf("%d sessions open, want one", len(open))
	}
	w := open[0]
	t.Cleanup(func() { unregisterWindow(w) })

	if err := openWindow(dir, source{}); err != nil {
		t.Fatal(err)
	}
	if n := len(sessionWindows()); n != 1 {
		t.Errorf("%d sessions open after opening it again, want the one", n)
	}
	if len(*went) != 1 || (*went)[0] != w {
		t.Errorf("opening it again focused %d sessions, want the one open", len(*went))
	}

	// Another target of the same repository is another session.
	gitIn(t, dir, "branch", "feature")
	if err := openWindow(dir, source{kind: sourceBranch, ref: "feature"}); err != nil {
		t.Fatal(err)
	}
	open = sessionWindows()
	t.Cleanup(func() { unregisterWindow(open[len(open)-1]) })
	if len(open) != 2 {
		t.Errorf("%d sessions open after another target, want the two", len(open))
	}
}

// TestWindowTitleNamesTheTarget checks the title the Window menu and the Dock
// list a window by: the repository, the target reviewed there, and the app.
func TestWindowTitleNamesTheTarget(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	dir := testRepo(t)
	gitIn(t, dir, "branch", "feature")
	head := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))
	name := filepath.Base(dir)

	w, _ := openTestSession(t, dir, source{})
	if title := windowTitle(w); !strings.HasPrefix(title, name+" — Working tree · ") {
		t.Errorf("the working tree's title is %q", title)
	}
	branch, _ := openTestSession(t, dir, source{kind: sourceBranch, ref: "feature"})
	if title := windowTitle(branch); !strings.Contains(title, "— Branch feature · ") {
		t.Errorf("the branch's title is %q", title)
	}
	commit, _ := openTestSession(t, dir, source{kind: sourceCommit, ref: head})
	if title := windowTitle(commit); !strings.Contains(title, "— Commit "+shortHash(head)+" · ") {
		t.Errorf("the commit's title is %q", title)
	}
	// A pull request names itself, and needs no remote for the title alone.
	branch.source = source{kind: sourcePull, ref: "acme/app#123"}
	if title := windowTitle(branch); !strings.Contains(title, "— PR #123 · ") {
		t.Errorf("the pull request's title is %q", title)
	}
}

// TestSessionsSpeakChinese checks the tab and its rows in the interface's
// language, without the window opening again.
func TestSessionsSpeakChinese(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	first, second := testRepo(t), testRepo(t)
	gitIn(t, second, "branch", "feature")
	a, tt := openTestSession(t, first, source{})
	openTestSession(t, second, source{kind: sourceBranch, ref: "feature"})
	a.tab = tabFiles
	a.applySetting(func(s *Settings) { s.UILocale = "zh-Hans" })
	tt.Frame()

	if err := tt.Click("会话 (⌘4)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"工作区", "分支 feature", "打开文件夹…", "最近", "关闭这个会话"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the Chinese sessions: %q", want, tt.Texts())
		}
	}
	for _, gone := range []string{"Working tree", "Branch feature", "Open Folder…", "Close this session"} {
		if tt.HasText(gone) {
			t.Errorf("%q stayed in the Chinese sessions: %q", gone, tt.Texts())
		}
	}
	if got := sessionTarget(source{kind: sourcePull, ref: "acme/app#123"}); got != "PR #123" {
		t.Errorf("a pull request says %q in Chinese, want PR #123", got)
	}
}

// TestRepoPanelListsOtherTargets checks the panel of the title bar over the
// same sessions the tab lists: the repository's other targets, which go to
// their windows, and the ways to another repository.
func TestRepoPanelListsOtherTargets(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	went := watchingFocus(t)
	dir := testRepo(t)
	gitIn(t, dir, "branch", "feature")
	a, tt := openTestSession(t, dir, source{})
	b, _ := openTestSession(t, dir, source{kind: sourceBranch, ref: "feature"})
	a.tab = tabFiles
	tt.Frame()

	if err := tt.Click("Switch repository"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Branch feature", "Open Folder…", "Reveal in Finder", "Close Repository"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the repository's panel: %q", want, tt.Texts())
		}
	}
	// The window's own target is not offered again.
	n := 0
	for _, text := range tt.Texts() {
		if text == "Working tree" {
			n++
		}
	}
	if n != 0 {
		t.Errorf("the panel lists the window's own target %d times: %q", n, tt.Texts())
	}
	if err := tt.Click("Branch feature"); err != nil {
		t.Fatal(err)
	}
	if len(*went) != 1 || (*went)[0] != b {
		t.Fatalf("the panel's target focused %d sessions, want the branch's one", len(*went))
	}
	if a.repoMenuOpen {
		t.Error("the panel stayed open after choosing a target")
	}
	// Escape closes it, as a press outside does.
	a.repoMenuOpen = true
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.repoMenuOpen {
		t.Error("Escape did not close the panel")
	}
}

// TestSessionsSnapshots saves the two surfaces of the sessions, in each
// language: the Sessions tab over two repositories, and the repository panel of
// the title bar.
func TestSessionsSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	useEnglish(t)
	noPlatformWindows(t)
	first, second := testRepo(t), testRepo(t)
	gitIn(t, second, "branch", "feature")
	a, tt := openTestSession(t, first, source{})
	openTestSession(t, second, source{kind: sourceBranch, ref: "feature"})
	markViewed(a, 1)
	for _, c := range []struct{ tag, prefix string }{{"en", "en"}, {"zh-Hans", "zh"}} {
		a.applySetting(func(s *Settings) { s.UILocale = c.tag })
		a.tab, a.repoMenuOpen, a.sidebarShown = tabSessions, false, true
		tt.Frame()
		tt.Frame()
		snapshot(t, tt, "sessions-"+c.prefix)

		a.tab, a.repoMenuOpen = tabFiles, true
		tt.Frame()
		tt.Frame()
		snapshot(t, tt, "repo-panel-"+c.prefix)
	}
}

// TestSessionsRecentRow checks the second of the two ways the tab keeps under
// its list: the repositories opened lately show where they are asked for.
func TestSessionsRecentRow(t *testing.T) {
	useEnglish(t)
	noPlatformWindows(t)
	state.clearRecent()
	for _, root := range []string{"/src/godiff", "/src/mygo"} {
		state.addRecent(root, maxRecentDefault)
	}
	t.Cleanup(state.clearRecent)
	a, tt := openTestSession(t, testRepo(t), source{})
	a.tab = tabSessions
	tt.Frame()

	if err := tt.Click("Recent"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.recentOpen {
		t.Fatal("the Recent row did not open")
	}
	for _, want := range []string{"godiff", "mygo"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not among the recent repositories: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Recent"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.recentOpen {
		t.Error("the Recent row did not close")
	}
}

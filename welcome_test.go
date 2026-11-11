package main

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// welcomeWith renders the welcome window over the recent repositories
// given, and returns the tester and the view.
func welcomeWith(t *testing.T, err error, roots ...string) (*welcome, *ui.Tester) {
	t.Helper()
	state.clearRecent()
	for i := len(roots) - 1; i >= 0; i-- {
		state.addRecent(roots[i], maxRecentDefault)
	}
	t.Cleanup(state.clearRecent)
	v := &welcome{err: err, choice: -1}
	tt := ui.NewTester(v.view, 680, 520)
	tt.Frame()
	return v, tt
}

func TestShowsWelcome(t *testing.T) {
	for _, c := range []struct {
		name     string
		args     []string
		fromUser bool
		want     bool
	}{
		{"the Dock, without an argument", nil, false, true},
		{"the Dock, with what macOS adds", []string{"-psn_0_1234"}, false, true},
		{"the Dock, on a folder", []string{"."}, true, false},
		{"a folder", []string{"/tmp"}, true, false},
		{"a revision", []string{"HEAD~1"}, true, false},
		{"a command line that names no argument, as godiff --cwd", nil, true, false},
		{"a platform argument on a command line", []string{"-psn_0_1"}, true, false},
	} {
		if got := showsWelcome(c.args, c.fromUser); got != c.want {
			t.Errorf("%s: showsWelcome(%q, %v) = %v, want %v", c.name, c.args, c.fromUser, got, c.want)
		}
	}
}

func TestWelcomeListsRecent(t *testing.T) {
	roots := []string{"/src/godiff", "/src/mygo", "/src/fate"}
	v, tt := welcomeWith(t, nil, roots...)
	snapshot(t, tt, "welcome")
	for _, root := range roots {
		if !tt.HasText(filepath.Base(root)) {
			t.Errorf("%s is not listed: %v", root, tt.Texts())
		}
		if !tt.HasText(filepath.Dir(root)) {
			t.Errorf("the path of %s is not listed", root)
		}
	}
	if !tt.HasText("Open Folder…") {
		t.Error("the welcome window cannot open a folder")
	}
	if tt.HasText("No repositories opened yet") {
		t.Error("the empty state shows with recent repositories")
	}
	if got := state.recent(); !slices.Equal(got, roots) {
		t.Errorf("recent = %v, want %v", got, roots)
	}
	// The keys choose a repository of the list.
	tt.Key(0, ui.KeyDown)
	if v.choice != 0 {
		t.Errorf("Down chose %d, want 0", v.choice)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	if v.choice != 0 {
		t.Errorf("Up chose %d, want 0", v.choice)
	}
}

func TestWelcomeWithoutRecent(t *testing.T) {
	_, tt := welcomeWith(t, nil)
	if !tt.HasText("No repositories opened yet — open a folder, or run godiff inside one.") {
		t.Errorf("the empty state is missing: %v", tt.Texts())
	}
	if !tt.HasText("Open Folder…") {
		t.Error("the welcome window cannot open a folder")
	}
	snapshot(t, tt, "welcome-empty")
}

func TestWelcomeErrors(t *testing.T) {
	_, tt := welcomeWith(t, git.ErrNotRepository)
	if !tt.HasText("No Git repository found") {
		t.Errorf("no title for a folder outside a repository: %v", tt.Texts())
	}
	_, tt = welcomeWith(t, errors.New("fatal: not a git repository"))
	if !tt.HasText("Unable to read repository") || !tt.HasText("fatal: not a git repository") {
		t.Errorf("the error is not shown: %v", tt.Texts())
	}
}

// speak makes the interface say tag, for the test, and puts back the language
// it said before.
func speak(t *testing.T, tag string) {
	t.Helper()
	was := interfaceLanguage
	interfaceLanguage = tag
	t.Cleanup(func() { interfaceLanguage = was })
}

// TestWelcomeSpeaksTheInterfaceLanguage checks the welcome page in each
// language: the page around a repository is the catalogs' too.
func TestWelcomeSpeaksTheInterfaceLanguage(t *testing.T) {
	roots := []string{"/src/godiff", "/src/mygo"}
	for _, c := range []struct {
		tag                           string
		subtitle, button, recent, run string
	}{
		{"en", "Review the changes of a repository, a commit, a branch, or a pull request.",
			"Open Folder…", "Recent", "Or run"},
		{"zh-Hans", "审查仓库、提交、分支或拉取请求里的改动。",
			"打开文件夹…", "最近", "也可以在仓库里运行"},
	} {
		t.Run(c.tag, func(t *testing.T) {
			speak(t, c.tag)
			_, tt := welcomeWith(t, nil, roots...)
			for _, want := range []string{c.subtitle, c.button, c.recent, c.run} {
				if !tt.HasText(want) {
					t.Errorf("%q is not in the welcome page: %q", want, tt.Texts())
				}
			}
		})
	}
}

// TestWelcomeEmptyAndErrorSpeakTheInterfaceLanguage checks the two states that
// say why no repository opened, in each language.
func TestWelcomeEmptyAndErrorSpeakTheInterfaceLanguage(t *testing.T) {
	speak(t, "zh-Hans")
	_, tt := welcomeWith(t, nil)
	if !tt.HasText("还没有打开过仓库——打开一个文件夹，或在仓库里运行 godiff。") {
		t.Errorf("the empty state is not in Chinese: %q", tt.Texts())
	}
	if !tt.HasText("打开文件夹…") {
		t.Errorf("the empty state cannot open a folder: %q", tt.Texts())
	}
	_, tt = welcomeWith(t, git.ErrNotRepository)
	if !tt.HasText("没有找到 Git 仓库") {
		t.Errorf("the error state is not in Chinese: %q", tt.Texts())
	}
	_, tt = welcomeWith(t, errors.New("fatal: not a git repository"))
	if !tt.HasText("无法读取仓库") || !tt.HasText("fatal: not a git repository") {
		t.Errorf("the raw failure is not shown: %q", tt.Texts())
	}
}

// TestWelcomeKeysOpenARecentRepository checks the keys of the recent ones: the
// arrows move the choice, and Return opens the row it is on.
func TestWelcomeKeysOpenARecentRepository(t *testing.T) {
	roots := []string{"/src/godiff", "/src/mygo"}
	v, tt := welcomeWith(t, nil, roots...)
	opened := make(chan string, 1)
	was := openFromWelcome
	openFromWelcome = func(root string) { opened <- root }
	t.Cleanup(func() { openFromWelcome = was })

	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	if v.choice != 0 {
		t.Fatalf("the arrows chose %d, want 0", v.choice)
	}
	tt.Key(0, ui.KeyEnter)
	select {
	case got := <-opened:
		if got != roots[0] {
			t.Errorf("Return opened %q, want %q", got, roots[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Return opened no repository")
	}
}

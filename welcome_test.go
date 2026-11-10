package main

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// welcomeWith renders the welcome window over the recent repositories
// given, and returns the tester and the view.
func welcomeWith(t *testing.T, err error, roots ...string) (*welcome, *ui.Tester) {
	t.Helper()
	state.clearRecent()
	for i := len(roots) - 1; i >= 0; i-- {
		state.addRecent(roots[i])
	}
	t.Cleanup(state.clearRecent)
	v := &welcome{err: err, choice: -1}
	tt := ui.NewTester(v.view, 760, 560)
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

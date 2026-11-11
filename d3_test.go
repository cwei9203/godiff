package main

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// TestToolbarNarrows checks what a narrow window drops first: the comments, then
// the search, and the View menu and the sidebar's switch after them.
func TestToolbarNarrows(t *testing.T) {
	w, tt := kindWindow(t)
	addText(w, "app.go", sideNew, 1, 1, "A comment.")
	tt.Frame()
	for _, c := range []struct {
		width         int
		search, count bool
	}{
		{1100, true, true},
		{900, true, false},
		{760, false, false},
	} {
		tt.SetSize(c.width, 720)
		tt.Frame()
		if got := tt.HasText(textFind + " (⌘F)"); got != c.search {
			t.Errorf("%d wide: search %v, want %v", c.width, got, c.search)
		}
		if got := tt.HasText("Copy review comments"); got != c.count {
			t.Errorf("%d wide: the comments %v, want %v", c.width, got, c.count)
		}
		for _, keep := range []string{textView, "Hide sidebar (⌘⇧B)"} {
			if !tt.HasText(keep) {
				t.Errorf("%d wide: %q went with them", c.width, keep)
			}
		}
	}
}

// TestD3Snapshots saves the frames of this batch, to look at: the toolbar it
// gathered, the command bar, the three states of the agent's card, and the bar
// of an undo.
func TestD3Snapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	w, tt := designWindow(t)
	tt.Frame()
	snapshot(t, tt, "toolbar-after")

	w.paletteOpen = true
	tt.Frame()
	snapshot(t, tt, "palette-open")
	w.paletteOpen = false

	// The card, offered and not started.
	st := w.analysisFor(w.source)
	result := st.result
	st.result, st.offered = nil, true
	w.regroup()
	tt.Frame()
	snapshot(t, tt, "ai-offer")

	// The card, at work.
	st.offered = false
	st.running = true
	st.agent = "Claude Code"
	st.started = w.now.Add(-42 * time.Second)
	st.progress = agent.Progress{Kind: agent.ProgressReading, Paths: []string{"app.go", "app_test.go", "go.mod"}}
	tt.Frame()
	snapshot(t, tt, "ai-running")

	// The card, with the review it gave.
	st.running, st.result = false, result
	tt.Frame()
	snapshot(t, tt, "ai-result")

	// The bar of an undo, after a fold of the whole tree.
	if err := tt.Click("Fold every group and folder"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "undo-bar")
}

// TestD3SnapshotsDark saves the same frames in the dark, where the palette has
// a branch of its own.
func TestD3SnapshotsDark(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	w, tt := designWindow(t)
	tt.SetDark(true)
	tt.Frame()
	snapshot(t, tt, "dark-toolbar")

	st := w.analysisFor(w.source)
	st.running, st.result = true, nil
	st.started = w.now
	st.agent = "Claude Code"
	st.progress = agent.Progress{Kind: agent.ProgressReading, Paths: []string{"app.go", "app_test.go"}}
	w.rowsDirty = true
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	snapshot(t, tt, "dark-ai-running")

	st.running = false
	st.result = &agent.Analysis{OverallSummary: "The change adds a log line.", Agent: "pi", GeneratedAt: time.Now(),
		Groups: []agent.Group{{Key: "code", Label: "Code", FilePaths: []string{"app.go"}}}}
	w.rowsDirty = true
	w.regroup()
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	if err := tt.Click("Fold every group and folder"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "dark-undo-bar")
}

// TestUndoFoldAll checks that a fold of the whole tree offers the way back, and
// that the way back puts every fold where it was, not where an opened tree
// would be.
func TestUndoFoldAll(t *testing.T) {
	w, tt := kindWindow(t)
	// A tree with some of it folded: the way back puts back that shape, not a
	// tree opened whole.
	w.closedDirs["g:docs"] = true
	before := w.folded()
	tt.Frame()
	if err := tt.Click("Fold every group and folder"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for key, n := range w.treeItems {
		if n != nil && n.dir && !w.closedDirs[key] {
			t.Fatalf("%s stayed open after the fold", key)
		}
	}
	if w.undo == nil {
		t.Fatal("the fold offers no undo")
	}
	if got, want := w.undo.label, "Folded all "+plural(len(w.groups), "group"); got != want {
		t.Errorf("the bar says %q, want %q", got, want)
	}
	if !undoShown(tt) {
		t.Fatalf("no undo in the bar: %q", tt.Texts())
	}
	if err := tt.Click(textUndo); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !mapsEqual(w.closedDirs, before.closedDirs) {
		t.Errorf("the undo left %v, want %v", w.closedDirs, before.closedDirs)
	}
	for i, f := range w.files {
		if f.collapsed != before.collapsed[i] {
			t.Errorf("%s: collapsed %v after the undo", f.Path, f.collapsed)
		}
	}
	if w.undo != nil || undoShown(tt) {
		t.Error("the bar stayed after its undo")
	}
}

// TestUndoMarkAllViewed checks the second one-tap action: marking every file
// viewed, and putting back the files that were not.
func TestUndoMarkAllViewed(t *testing.T) {
	w, tt := kindWindow(t)
	// One file viewed already, which the undo keeps viewed.
	first := w.files[0]
	w.setViewed(first, true)
	before := mapsEqual
	_ = before
	viewed := len(w.viewed)
	tt.Frame()
	w.markAllViewed()
	tt.Frame()
	if len(w.viewed) != len(w.files) {
		t.Fatalf("%d files viewed, want %d", len(w.viewed), len(w.files))
	}
	if w.undo == nil {
		t.Fatal("marking them viewed offers no undo")
	}
	if err := tt.Click(textUndo); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(w.viewed) != viewed {
		t.Errorf("%d files viewed after the undo, want %d", len(w.viewed), viewed)
	}
	if !w.isViewed(first) {
		t.Errorf("%s is no longer viewed", first.Path)
	}
	for _, f := range w.files {
		if f == first {
			continue
		}
		if w.isViewed(f) {
			t.Errorf("%s stayed viewed", f.Path)
		}
	}
}

// TestUndoBarGoesAfterItsGrace checks that the bar of an undo goes by itself,
// and that it goes by undoGrace: the tester cannot move MyGo's clock, so the
// action is dated further back than the grace instead.
func TestUndoBarGoesAfterItsGrace(t *testing.T) {
	if undoGrace != 5*time.Second {
		t.Fatalf("the bar stays %v, want five seconds", undoGrace)
	}
	w, tt := kindWindow(t)
	w.foldAll(true)
	tt.Frame()
	if w.undo == nil {
		t.Fatal("no bar after the fold")
	}
	w.undo.at = w.undo.at.Add(-undoGrace - time.Millisecond)
	tt.Frame()
	if w.undo != nil || undoShown(tt) {
		t.Error("the bar stayed past its grace")
	}
}

// TestUndoBarEscapeDismisses checks that Escape closes the bar without undoing
// the action it offered back.
func TestUndoBarEscapeDismisses(t *testing.T) {
	w, tt := kindWindow(t)
	w.focusList = true
	tt.Frame()
	before := w.folded()
	w.foldAll(true)
	tt.Frame()
	if w.undo == nil {
		t.Fatal("no bar after the fold")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if w.undo != nil || undoShown(tt) {
		t.Fatal("Escape kept the bar")
	}
	if mapsEqual(w.closedDirs, before.closedDirs) {
		t.Error("Escape undid the fold")
	}
	if !w.closedDirs["g:docs"] {
		t.Error("Escape opened the fold it left")
	}
}

// TestUndoBarKeepsTheLastAction checks that continuous actions keep one bar:
// the offer is the last action's alone.
func TestUndoBarKeepsTheLastAction(t *testing.T) {
	w, tt := kindWindow(t)
	w.foldAll(true)
	tt.Frame()
	w.markAllViewed()
	tt.Frame()
	if w.undo == nil {
		t.Fatal("no bar after the marked files")
	}
	if got, want := w.undo.label, "Marked all "+plural(len(w.files), "file")+" viewed"; got != want {
		t.Errorf("the bar says %q, want %q", got, want)
	}
	if !undoShown(tt) {
		t.Error("the last action offers nothing back")
	}
}

// undoShown reports whether the bar of an undo is on screen. It looks for the
// bar's own word exactly: the path of a test's repository holds the name of the
// test, and a test named after an undo would find itself.
func undoShown(tt *ui.Tester) bool { return slices.Contains(tt.Texts(), textUndo) }

// TestPaletteHasTheCommands checks the commands the bar holds, and that the
// ones the review does not have yet are there and do nothing.
func TestPaletteHasTheCommands(t *testing.T) {
	w, _ := kindWindow(t)
	w.settings.FileView = fileViewOne
	addText(w, w.files[0].Path, sideNew, 1, 1, "A comment to fix.")
	want := []string{
		"Next unviewed file",
		"Fold all",
		"Unfold all",
		"Export review task",
		"Fix comments with an agent",
		"Show one file at a time",
		"Show all files",
		"Settings",
	}
	for _, title := range want {
		if _, ok := findCommand(w, title); !ok {
			t.Errorf("no command %q", title)
		}
	}
	// The settings window is not built: its command says so, and runs nothing.
	cmd, _ := findCommand(w, "Settings")
	if !cmd.soon || cmd.run != nil || cmd.hint != textSettingsSoon {
		t.Errorf("the settings command is %+v, want one that is to come", cmd)
	}
	// The keys are the ones the keys are: n steps the files one at a time.
	if cmd, _ := findCommand(w, "Next unviewed file"); cmd.keys != "N" {
		t.Errorf("next unviewed file shows %q, want N", cmd.keys)
	}
	if cmd, _ := findCommand(w, "Next AI note"); cmd.keys != "⌥N" {
		t.Errorf("next AI note shows %q, want ⌥N", cmd.keys)
	}
}

// TestPaletteNextUnviewedFile checks the command itself: it shows the next file
// not viewed yet, as n does.
func TestPaletteNextUnviewedFile(t *testing.T) {
	w, tt := kindWindow(t)
	keepFileView(t)
	w.setFileView(fileViewOne)
	w.current = 0
	tt.Frame()
	cmd, ok := findCommand(w, "Next unviewed file")
	if !ok || cmd.run == nil {
		t.Fatal("no command to go to the next unviewed file")
	}
	cmd.run()
	tt.Frame()
	if w.current == 0 {
		t.Errorf("%s still shows, and the next file was not viewed", w.files[w.current].Path)
	}
	if !w.isViewed(w.files[0]) {
		// Nothing was marked: the file shown moved alone.
		t.Logf("the file shown is %s", w.files[w.current].Path)
	}
	// A file already viewed is stepped over.
	w.setViewed(w.files[1], true)
	w.current = 0
	cmd.run()
	tt.Frame()
	if w.current == 1 {
		t.Errorf("%s is viewed, and the command showed it", w.files[1].Path)
	}
}

// TestAICardThreeStates checks the one action each state of the agent's card
// carries: the offer, the stop, and the review again.
func TestAICardThreeStates(t *testing.T) {
	w, tt := kindWindow(t)
	st := w.analysisFor(w.source)
	st.offered = true
	w.regroup()
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	if !tt.HasText(textReviewWithAI) {
		t.Fatalf("the offered card has no button to review: %q", tt.Texts())
	}
	if tt.HasText(textStop) || tt.HasText(textReviewAgain) {
		t.Error("the offered card carries the actions of another state")
	}

	st.offered, st.running = false, true
	st.agent = "Claude Code"
	st.started = w.now
	st.progress = agent.Progress{Kind: agent.ProgressOrganizing}
	w.rowsDirty = true
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	if !tt.HasText(textStop) {
		t.Fatalf("the card at work cannot be stopped: %q", tt.Texts())
	}
	if tt.HasText(textReviewWithAI) || tt.HasText(textReviewAgain) {
		t.Error("the card at work carries the actions of another state")
	}
	if !tt.HasText(textOrganizing) {
		t.Errorf("the card at work does not say its step: %q", tt.Texts())
	}

	st.running = false
	st.result = &agent.Analysis{
		OverallSummary: "The change adds a log line and a test.",
		Agent:          "pi",
		Model:          "sonnet",
		GeneratedAt:    time.Now(),
		Groups:         []agent.Group{{Key: "code", Label: "Code", FilePaths: []string{"app.go"}}},
	}
	w.regroup()
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	if !tt.HasText(textReviewAgain) {
		t.Fatalf("the card of a review cannot review again: %q", tt.Texts())
	}
	if tt.HasText(textReviewWithAI) || tt.HasText(textStop) {
		t.Error("the card of a review carries the actions of another state")
	}
	// Where the review comes from shows in the card, as its note.
	if !tt.HasText("Pi · sonnet · just now") {
		t.Errorf("the card does not say where the review comes from: %q", tt.Texts())
	}
}

// TestProgressLineFollowsTheStep checks that the card says what the agent does
// now, rather than one sentence through the run.
func TestProgressLineFollowsTheStep(t *testing.T) {
	w, tt := kindWindow(t)
	st := w.analysisFor(w.source)
	st.offered, st.running, st.started = false, true, w.now
	w.rowsDirty = true
	w.list.ScrollTo(0, ui.Start)
	lines := []struct {
		progress agent.Progress
		want     string
	}{
		{agent.Progress{Kind: agent.ProgressThinking}, textThinking},
		{agent.Progress{Kind: agent.ProgressReading, Paths: []string{"app.go", "go.mod"}}, "Reading 2 files"},
		{agent.Progress{Kind: agent.ProgressOrganizing}, textOrganizing},
	}
	for _, line := range lines {
		st.progress = line.progress
		w.rowsDirty = true
		w.list.ScrollTo(0, ui.Start)
		tt.Frame()
		if !tt.HasText(line.want) {
			t.Errorf("%s: %q is not in the card: %q", line.progress.Kind, line.want, tt.Texts())
		}
	}
}

// TestTabWalksThePanels checks the order the keys walk: the tree, the review,
// the drawer, and the toolbar, which is built last for it.
func TestTabWalksThePanels(t *testing.T) {
	w, tt := kindWindow(t)
	w.fix = &fixState{open: true, phase: fixRunning, agent: "Claude Code", started: w.now}
	tt.Frame()
	// The tree takes the focus from a click, and the keys walk from it to the
	// review, to the drawer, and to the bar, in that order.
	box, ok := tt.Find("README.md")
	if !ok {
		t.Fatalf("no README.md: %q", tt.Texts())
	}
	tt.ClickAt(box.X+4, box.Y+box.H/2)
	tt.Frame()
	if !tt.Focused("Changed files") {
		t.Fatal("the tree did not take the focus")
	}
	// The stops of the panels, in the order the keys reach them: each panel
	// has stops of its own, and the panels keep their order.
	region := func(s string) string {
		switch s {
		case "Changed files", "Commit", "Files (⌘1)", "History (⌘2)", "Filter files":
			return "sidebar"
		case "Changes", "Next file (])", "Next unviewed file", "Viewed", "Copy path", "Comment":
			return "main"
		case "The agent's output", "Stop", "Hide the agent's output":
			return "drawer"
		case textRepo, textFind + " (⌘F)", textView, "Hide sidebar (⌘⇧B)":
			return "bar"
		}
		return ""
	}
	var walked []string
	for range 200 {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		if r := region(focusedText(tt)); r != "" && (len(walked) == 0 || walked[len(walked)-1] != r) {
			walked = append(walked, r)
		}
	}
	want := []string{"main", "drawer", "bar", "sidebar"}
	if !slices.Contains(walked, "main") || !slices.Contains(walked, "drawer") || !slices.Contains(walked, "bar") {
		t.Fatalf("the keys walk %v, want the review, the drawer and the bar", walked)
	}
	// From wherever the walk starts, the panels keep their order, round from
	// the last to the first.
	at := slices.Index(walked, "main")
	for i, r := range want {
		if got := walked[(at+i)%len(walked)]; got != r {
			t.Fatalf("the keys walk %v: %s comes where %s should", walked, got, r)
		}
	}
}

// focusedText names what has the focus, for a test that fails.
func focusedText(tt *ui.Tester) string {
	for _, text := range tt.Texts() {
		if tt.Focused(text) {
			return text
		}
	}
	return ""
}

// TestFocusRingOnlyFromTheKeyboard checks the one ring of the review: it shows
// where the keys put the focus, and a click leaves it off.
func TestFocusRingOnlyFromTheKeyboard(t *testing.T) {
	_, tt := kindWindow(t)
	tt.Frame()
	box, ok := tt.Find("Changes")
	if !ok {
		t.Fatalf("no review: %q", tt.Texts())
	}
	// The ring lies on the edge of the surface, inside it.
	at := func(x, y float32) [4]uint32 {
		r, g, b, a := tt.Image().RGBAAt(int(x), int(y)).RGBA()
		return [4]uint32{r, g, b, a}
	}
	probe := func() [4]uint32 { return at(box.X+1, box.Y+box.H/2) }
	unringed := probe()

	// The review has the focus as the window opens, which is no ring: the
	// keys must bring the focus there for one to show.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	for range 120 {
		if tt.Focused("Changes") {
			break
		}
		tt.Key(0, ui.KeyTab)
		tt.Frame()
	}
	if !tt.Focused("Changes") {
		t.Fatalf("the keys never reach the review: %q", focusedText(tt))
	}
	ring := probe()
	if ring == unringed {
		t.Fatalf("the keys left no ring: %v", ring)
	}
	// The palette's focus is a blue, and the surface behind it is not.
	if r, g, b := ring[0], ring[1], ring[2]; b <= r || b <= g {
		t.Errorf("the ring shows %v, which is no blue", ring)
	}

	// A click on a file of the tree moves the focus there, and leaves no ring
	// on the review.
	row, ok := tt.Find("README.md")
	if !ok {
		t.Fatal("no README.md")
	}
	tt.ClickAt(row.X+4, row.Y+row.H/2)
	tt.Move(1, 1)
	tt.Frame()
	if got := probe(); got != unringed {
		t.Errorf("the review still rings %v after a click, where it showed %v", got, unringed)
	}
}

// mapsEqual reports whether two maps hold the same pairs.
func mapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// TestResetReviewState checks the third one-tap action: nothing viewed and
// everything open, with the way back.
func TestResetReviewState(t *testing.T) {
	w, tt := kindWindow(t)
	w.setViewed(w.files[0], true)
	w.closedDirs["g:docs"] = true
	viewed, closed := len(w.viewed), len(w.closedDirs)
	tt.Frame()
	w.resetReviewState()
	tt.Frame()
	if len(w.viewed) != 0 || len(w.closedDirs) != 0 {
		t.Fatalf("%d viewed and %d folded after the reset", len(w.viewed), len(w.closedDirs))
	}
	for _, f := range w.files {
		if f.collapsed {
			t.Errorf("%s stayed collapsed", f.Path)
		}
	}
	if w.undo == nil {
		t.Fatal("the reset offers no undo")
	}
	if err := tt.Click(textUndo); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(w.viewed) != viewed || len(w.closedDirs) != closed {
		t.Errorf("%d viewed and %d folded after the undo, want %d and %d", len(w.viewed), len(w.closedDirs), viewed, closed)
	}
	if !slices.ContainsFunc(w.commands(), func(c command) bool { return c.title == "Reset review state" && c.run != nil }) {
		t.Error("no command to reset the review")
	}
}

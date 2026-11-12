package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/review"
)

// TestLogRing keeps the last lines of a run: the oldest go, the newest stay,
// in the order they came.
func TestLogRing(t *testing.T) {
	var r logRing
	for i := 1; i <= 3; i++ {
		r.add(fmt.Sprintf("line %d", i))
	}
	if r.lines() != 3 {
		t.Fatalf("lines: %d", r.lines())
	}
	for i, want := range []string{"line 1", "line 2", "line 3"} {
		if got := r.line(i); got.text != want || got.seq != i+1 {
			t.Fatalf("line %d: %+v, want %q with seq %d", i, got, want, i+1)
		}
	}

	// Past the limit the oldest are dropped, and the rest keep their order.
	overflow := 5
	for i := 4; i <= fixLogLines+overflow; i++ {
		r.add(fmt.Sprintf("line %d", i))
	}
	if r.lines() != fixLogLines {
		t.Fatalf("lines: %d, want %d", r.lines(), fixLogLines)
	}
	if got := r.line(0); got.text != fmt.Sprintf("line %d", overflow+1) {
		t.Errorf("the oldest is %q, want line %d", got.text, overflow+1)
	}
	if got := r.line(r.lines() - 1); got.text != fmt.Sprintf("line %d", fixLogLines+overflow) {
		t.Errorf("the newest is %q, want line %d", got.text, fixLogLines+overflow)
	}
	for i := range r.lines() {
		if got, want := r.line(i).seq, overflow+1+i; got != want {
			t.Fatalf("line %d has seq %d, want %d: the order is not the one they came in", i, got, want)
		}
	}
}

var errAgent = errors.New("claude failed with exit code 2: no such file")

// TestEndFix is the drawer's state machine: a run ends stopped, succeeded or
// failed, and stopped is never a failure.
func TestEndFix(t *testing.T) {
	res := review.FixResult{
		ExitCode: 0,
		Duration: 2*time.Minute + 13*time.Second,
		Changed:  []string{"main.go", "src/new.go"},
		LogPath:  "/home/ada/.godiff/runs/fix-1.log",
	}

	end := endFix(res, nil, false)
	if end.phase != fixSucceeded {
		t.Errorf("a run that worked: phase %v", end.phase)
	}
	if end.exitCode != 0 || end.duration != res.Duration || len(end.changed) != 2 || end.logPath != res.LogPath {
		t.Errorf("a run that worked ends with %+v", end)
	}
	if end.errText != "" {
		t.Errorf("a run that worked says %q", end.errText)
	}

	// The user stopped it: what it did is kept, and none of it is a failure.
	stopped := res
	stopped.ExitCode = -1
	end = endFix(stopped, errAgent, true)
	if end.phase != fixStopped {
		t.Errorf("a stopped run: phase %v, want stopped", end.phase)
	}
	if end.errText != "" {
		t.Errorf("a stopped run shows an error: %q", end.errText)
	}
	if len(end.changed) != 2 || end.logPath == "" {
		t.Errorf("a stopped run lost what it did: %+v", end)
	}

	// A failure shows the tail of what the agent wrote to stderr.
	failed := res
	failed.ExitCode = 2
	failed.ErrDetail = "building…\nmain.go:3: undefined: name"
	end = endFix(failed, errAgent, false)
	if end.phase != fixFailed {
		t.Errorf("a run that failed: phase %v", end.phase)
	}
	if end.errText != failed.ErrDetail {
		t.Errorf("the error shown is %q, want %q", end.errText, failed.ErrDetail)
	}

	// A failure godiff knows shows the sentence to act on, not its raw words.
	explained := res
	explained.ExitCode = 1
	explained.ErrReason = agent.ReasonNotSignedIn
	explained.ErrDetail = "Error: not logged in\nSee /opt/agent/docs/login.md"
	end = endFix(explained, errAgent, false)
	if want := msg().ExplainNotSignedIn(); end.errText != want {
		t.Errorf("the error shown is %q, want %q", end.errText, want)
	}

	// An agent the way to let write is unverified is refused with a sentence
	// too: unverified is not unable.
	unverified := res
	unverified.ErrReason = agent.ReasonNotVerified
	end = endFix(unverified, errAgent, false)
	if want := msg().ExplainNotVerified(); end.errText != want {
		t.Errorf("the error shown is %q, want %q", end.errText, want)
	}

	// A failure that wrote nothing to stderr shows the error itself, and the
	// long tail of it is cut to the lines the card shows.
	end = endFix(review.FixResult{ExitCode: -1}, errAgent, false)
	if end.phase != fixFailed || end.errText != errAgent.Error() {
		t.Errorf("a failure without stderr: phase %v, error %q", end.phase, end.errText)
	}
	var many strings.Builder
	for i := range fixFailedLines * 3 {
		fmt.Fprintf(&many, "error %d\n", i)
	}
	end = endFix(review.FixResult{ErrDetail: many.String()}, errAgent, false)
	if n := strings.Count(end.errText, "\n") + 1; n != fixFailedLines {
		t.Errorf("the tail is %d lines, want %d:\n%s", n, fixFailedLines, end.errText)
	}
	if !strings.Contains(end.errText, fmt.Sprintf("error %d", fixFailedLines*3-1)) {
		t.Errorf("the tail is not the end:\n%s", end.errText)
	}
}

// TestFixHeadline names a run in one line, the stopped one as stopped.
func TestFixHeadline(t *testing.T) {
	started := time.Now().Add(-2*time.Minute - 13*time.Second)
	running := &fixState{phase: fixRunning, agent: "Claude Code", started: started}
	if got := running.headline(time.Now()); !strings.HasPrefix(got, "Agent fixing · Claude Code · 2m1") {
		t.Errorf("running: %q", got)
	}
	running.stopped = true
	if got := running.headline(time.Now()); !strings.HasPrefix(got, "Stopping the agent") {
		t.Errorf("stopping: %q", got)
	}
	stopped := &fixState{phase: fixStopped, agent: "Claude Code", duration: 2 * time.Minute}
	if got := stopped.headline(time.Now()); got != "Stopped · Claude Code · 2m0s" {
		t.Errorf("stopped: %q", got)
	}
	done := &fixState{phase: fixSucceeded, agent: "Claude Code", comments: 4, changed: []string{"a.go", "b.go"}}
	if got := done.headline(time.Now()); got != "Fixed 4 comments · 2 changed" {
		t.Errorf("fixed: %q", got)
	}
	none := &fixState{phase: fixSucceeded, comments: 1}
	if got := none.headline(time.Now()); !strings.Contains(got, "no file changed") {
		t.Errorf("fixed nothing: %q", got)
	}
	failed := &fixState{phase: fixFailed, exitCode: 2, duration: time.Second}
	if got := failed.headline(time.Now()); got != "Fix failed · exit code 2 · 1s" {
		t.Errorf("failed: %q", got)
	}
}

// TestFixLine reaches the main thread through the window's update, and keeps
// the lines that came with the one that crossed in order.
func TestFixLine(t *testing.T) {
	w := &window{}
	st := &fixState{phase: fixRunning}
	// A window without one runs the update at once, as tests do; several
	// lines arriving together must all land, once each.
	for _, l := range []string{"first", "second", "third"} {
		w.fixLine(st, l)
	}
	if st.ring.lines() != 3 {
		t.Fatalf("lines: %d", st.ring.lines())
	}
	for i, want := range []string{"first", "second", "third"} {
		if got := st.ring.line(i).text; got != want {
			t.Errorf("line %d: %q, want %q", i, got, want)
		}
	}
	if st.flushing || len(st.pending) != 0 {
		t.Errorf("lines are left pending: %d lines, flushing %v", len(st.pending), st.flushing)
	}
}

// TestFixDrawerToStart runs a state machine of its own: a repair starts on the
// goroutine of the backend and ends on the main thread, as the drawer shows.
func TestFixDoneShowsTheResult(t *testing.T) {
	w := &window{}
	st := &fixState{phase: fixRunning, agent: "Claude Code", started: time.Now()}
	w.fix = st
	w.fixDone(st, review.FixResult{ExitCode: 0, Duration: time.Second, Changed: []string{"main.go"}, LogPath: "/tmp/fix.log"}, nil)
	if st.phase != fixSucceeded {
		t.Fatalf("phase: %v", st.phase)
	}
	if st.logPath != "/tmp/fix.log" || len(st.changed) != 1 || st.stop != nil {
		t.Errorf("the result is %+v", st)
	}
	// An end for a run the window no longer shows changes nothing.
	other := &fixState{phase: fixRunning}
	w.fixDone(other, review.FixResult{}, errAgent)
	if other.phase != fixRunning {
		t.Errorf("a run not shown ended as %v", other.phase)
	}
}

// TestFixDrawer draws the drawer: folded to a line, open on the log, and the
// result once the run ended.
func TestFixDrawer(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	if tt.HasText("Agent fixing") {
		t.Fatal("a drawer shows with no repair")
	}
	st := &fixState{phase: fixRunning, agent: "Claude Code", started: time.Now()}
	st.ring.add("go test ./...")
	st.ring.add("FAIL  github.com/egoist/godiff")
	w.fix = st
	tt.Frame()
	if !tt.HasText("Agent fixing · Claude Code") {
		t.Fatalf("the drawer's line is missing from %q", tt.Texts())
	}
	if tt.HasText("FAIL  github.com") {
		t.Error("the log shows while the drawer is folded")
	}
	if err := tt.Click("Show the agent's output"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("FAIL  github.com") {
		t.Fatalf("the open drawer shows no log: %q", tt.Texts())
	}
	if !tt.HasText("go test ./...") {
		t.Error("the first line of the log is missing")
	}

	// Ended, it shows the result: the exit code, the files, and the two
	// actions, with no stop button.
	st.phase, st.exitCode, st.duration = fixSucceeded, 0, 90*time.Second
	st.changed, st.logPath = []string{"main.go", "src/new.go"}, "/tmp/fix.log"
	tt.Frame()
	for _, want := range []string{"Fixed", "Exit code 0", "took 1m30s", "main.go, src/new.go", "Open the Log File", "Refresh Diff"} {
		if !tt.HasText(want) {
			t.Errorf("the result shows no %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Stop") {
		t.Error("a run that ended still offers to stop it")
	}

	// Stopped is stopped, not failed.
	st.phase, st.stopped, st.errText = fixStopped, true, ""
	tt.Frame()
	if !tt.HasText("Stopped ·") {
		t.Errorf("a stopped run is not named stopped: %q", tt.Texts())
	}
	if tt.HasText("Fix failed") {
		t.Error("a stopped run shows as a failure")
	}

	// A run that changed nothing says so, and a failure shows its tail.
	st.phase, st.changed, st.errText = fixFailed, nil, "main.go:3: undefined: name"
	tt.Frame()
	if !tt.HasText("No files were changed") {
		t.Errorf("no %q in %q", "No files were changed", tt.Texts())
	}
	if !tt.HasText("Fix failed · exit code 0") {
		t.Errorf("no failure line in %q", tt.Texts())
	}
	if !tt.HasText("undefined: name") {
		t.Errorf("the failure detail is missing from %q", tt.Texts())
	}

	// Folded, the drawer is its one line: the card and the log go with the
	// fold.
	st.open = false
	tt.Frame()
	if !tt.HasText("Fix failed") {
		t.Errorf("the folded drawer lost its line: %q", tt.Texts())
	}
	for _, gone := range []string{"Refresh Diff", "undefined: name", "FAIL  github.com"} {
		if tt.HasText(gone) {
			t.Errorf("the folded drawer still shows %q", gone)
		}
	}
}

// TestFixDialog asks for what a repair needs before it starts: the agent, its
// model and the range, with the repair's agent first, else the reviewer's.
func TestFixDialog(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	addText(w, "main.go", sideNew, 3, 3, "Name it for what it returns.")
	w.settings.FixAgent, w.settings.FixModel, w.settings.AIAgent, w.settings.AIModel = "", "", "codex", "gpt-5"
	w.openFixDialog()
	if !w.fixChoice.open {
		t.Fatal("the dialog did not open")
	}
	if w.fixChoice.agent != "codex" || w.fixChoice.model != "gpt-5" || w.fixChoice.scope != fixScopeAll {
		t.Errorf("the defaults are %+v", w.fixChoice)
	}
	tt.Frame()
	for _, want := range []string{"Fix Review Comments", "All Comments", "This Group", "This File", "Start the Agent"} {
		if !tt.HasText(want) {
			t.Errorf("the dialog shows no %q in %q", want, tt.Texts())
		}
	}
	// A repair of its own names its own agent and model.
	w.fixChoice.open = false
	w.settings.FixAgent, w.settings.FixModel = "claude", ""
	w.openFixDialog()
	if w.fixChoice.agent != "claude" || w.fixChoice.model != "gpt-5" {
		t.Errorf("the repair's defaults are %+v", w.fixChoice)
	}
}

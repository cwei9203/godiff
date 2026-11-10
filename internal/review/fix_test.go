//go:build !windows

package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
)

// fixRepo is a repository with one committed file, for a run to change.
func fixRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Ada")
	gitIn(t, dir, "config", "user.email", "ada@example.com")
	write(t, dir, "tracked.txt", "one\ntwo\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "First commit")
	return dir
}

// fakeAgent puts a script in place of an agent's command, first in the PATH,
// and returns where the script is told to write its arguments and its stdin.
// It changes the work tree, prints two lines and a warning, and exits 3.
func fakeAgent(t *testing.T, name, script string) (args, stdin string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	args, stdin = filepath.Join(dir, "args"), filepath.Join(dir, "stdin")
	t.Setenv("FAKE_ARGS", args)
	t.Setenv("FAKE_STDIN", stdin)
	// The app's own PATH alone, which is this one: no login shell of the
	// machine may put a real agent first.
	t.Setenv("GODIFF_NO_SHELL_PATH", "1")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	return args, stdin
}

// fixingAgent is a fake agent that repairs the task the way a real one
// would: it reads it, changes a tracked file, adds one, and fails.
const fixingAgent = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_ARGS"
cat > "$FAKE_STDIN"
echo "first line"
echo "second line"
echo "a warning" >&2
echo changed >> tracked.txt
echo new > added.txt
exit 3
`

func write(t *testing.T, dir, path, text string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// runFix starts a repair and waits for it, collecting its lines.
func runFix(t *testing.T, req FixRequest) (FixResult, error, []string) {
	t.Helper()
	done := make(chan struct{})
	var res FixResult
	var err error
	var lines []string
	var mu sync.Mutex
	Start(context.Background(), req,
		func(line string) {
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		},
		func(r FixResult, e error) {
			res, err = r, e
			close(done)
		})
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not end")
	}
	mu.Lock()
	defer mu.Unlock()
	return res, err, lines
}

func TestFixPermissions(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		// want are the arguments the repair needs, in order, and ban are
		// what the analysis passes and the repair may not.
		want []string
		ban  [][]string
	}{
		{"claude", "claude", []string{"-p", "--permission-mode", "acceptEdits"}, [][]string{
			{"--bare"}, {"--allowedTools"}, {"--disallowedTools"}, {"plan"},
		}},
		{"codex", "codex", []string{"-s", "workspace-write"}, [][]string{
			{"read-only"},
		}},
		{"pi", "pi", []string{"-p"}, [][]string{
			{"--tools"}, {"--no-context-files"}, {"--no-skills"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsPath, stdinPath := fakeAgent(t, tt.agent, fixingAgent)
			root := fixRepo(t)
			task := "# Review: one comment\n\n- `tracked.txt`: fix it\n"
			a, _ := agent.ByName(tt.agent)
			res, err, _ := runFix(t, FixRequest{Root: root, Agent: a, Model: "some-model", Task: task})
			if err == nil {
				t.Fatalf("the run reported no failure, exit code %d", res.ExitCode)
			}
			raw, readErr := os.ReadFile(argsPath)
			if readErr != nil {
				t.Fatalf("the agent was not run: %v", readErr)
			}
			args := strings.Fields(string(raw))
			if !hasRun(args, tt.want) {
				t.Errorf("no %v in the arguments: %v", tt.want, args)
			}
			if !slices.Contains(args, "some-model") {
				t.Errorf("no model in the arguments: %v", args)
			}
			for _, ban := range tt.ban {
				if hasRun(args, ban) {
					t.Errorf("%v is in the arguments, which only the analysis may have: %v", ban, args)
				}
			}
			// The prompt goes on stdin, the task file inside the repository
			// holds the same text.
			if got, _ := os.ReadFile(stdinPath); string(got) != task {
				t.Errorf("the agent read %q, not the task", got)
			}
			if got := reading(t, Path(root)); got != task {
				t.Errorf("the task file holds %q, not the task", got)
			}
		})
	}
}

// hasRun reports whether a list of arguments holds a run of runs.
func hasRun(args, run []string) bool {
	for i := 0; i+len(run) <= len(args); i++ {
		if slices.Equal(args[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

func reading(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no %s: %v", path, err)
	}
	return string(data)
}

func TestFixResult(t *testing.T) {
	fakeAgent(t, "claude", fixingAgent)
	root := fixRepo(t)
	a, _ := agent.ByName("claude")
	res, err, lines := runFix(t, FixRequest{Root: root, Agent: a, Task: "# Review\n\n- `tracked.txt`: fix it\n"})
	if err == nil {
		t.Fatal("a run that exits 3 reported no error")
	}
	// The failure says how it failed, and what the agent's errors said.
	if !strings.Contains(err.Error(), "code 3") || !strings.Contains(err.Error(), "a warning") {
		t.Errorf("error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit code: %d", res.ExitCode)
	}
	if res.Duration <= 0 {
		t.Errorf("duration: %v", res.Duration)
	}
	if !strings.Contains(res.ErrDetail, "a warning") {
		t.Errorf("detail: %q", res.ErrDetail)
	}
	// Stdout arrives a line at a time, in order.
	if want := []string{"first line", "second line"}; !slices.Equal(lines, want) {
		t.Errorf("lines: %q", lines)
	}
	// The changed files are git's, the task file aside.
	if want := []string{"added.txt", "tracked.txt"}; !slices.Equal(sorted(res.Changed), want) {
		t.Errorf("changed: %q", res.Changed)
	}
	if slices.ContainsFunc(res.Changed, isTaskPath) {
		t.Errorf("the task file counts as a change: %q", res.Changed)
	}
	// The whole output is in the log, paths and warning too.
	log := reading(t, res.LogPath)
	if !filepath.IsAbs(res.LogPath) || !strings.HasPrefix(res.LogPath, os.Getenv("HOME")) {
		t.Errorf("log path: %s", res.LogPath)
	}
	for _, want := range []string{"first line", "second line", "a warning", "acceptEdits"} {
		if !strings.Contains(log, want) {
			t.Errorf("no %q in the log:\n%s", want, log)
		}
	}
}

func TestFixExcludesTheTaskFile(t *testing.T) {
	// A repository whose only change is the task file: it is godiff's own,
	// so nothing changed.
	fakeAgent(t, "claude", `#!/bin/sh
cat > "$FAKE_STDIN"
exit 0
`)
	root := fixRepo(t)
	a, _ := agent.ByName("claude")
	res, err, _ := runFix(t, FixRequest{Root: root, Agent: a, Task: "# Review\n\n- `tracked.txt`: fix it\n"})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	if len(res.Changed) != 0 {
		t.Errorf("changed: %q", res.Changed)
	}
	if _, err := os.Stat(Path(root)); err != nil {
		t.Fatalf("the task file was not written: %v", err)
	}
	// Anything below .review/ is godiff's, the task file or a note the agent
	// left beside it; a path that only looks like one is not.
	for _, path := range []string{".review", ".review/", ".review/REVIEW.md", ".review/notes.md"} {
		if !isTaskPath(path) {
			t.Errorf("%s is not the task's", path)
		}
	}
	for _, path := range []string{"review/REVIEW.md", ".review.md", "src/.review/x.go", "main.go"} {
		if isTaskPath(path) {
			t.Errorf("%s is the task's", path)
		}
	}
}

// sorted is a list of paths in order, as git's order is not the test's.
func sorted(paths []string) []string {
	out := slices.Clone(paths)
	slices.Sort(out)
	return out
}

func TestFixStops(t *testing.T) {
	fakeAgent(t, "claude", `#!/bin/sh
cat > "$FAKE_STDIN"
echo started
exec sleep 60
`)
	root := fixRepo(t)
	a, _ := agent.ByName("claude")
	lines := make(chan string, 4)
	done := make(chan struct{})
	var res FixResult
	var err error
	stop := Start(context.Background(), FixRequest{Root: root, Agent: a, Task: "# Review\n"},
		func(line string) { lines <- line },
		func(r FixResult, e error) { res, err = r, e; close(done) })
	select {
	case line := <-lines:
		if line != "started" {
			t.Fatalf("first line: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run printed nothing")
	}
	start := time.Now()
	stop()
	select {
	case <-done:
	case <-time.After(stopGrace + time.Second):
		t.Fatal("the run outlived its stop")
	}
	if d := time.Since(start); d > stopGrace {
		t.Errorf("the run took %v to stop", d)
	}
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Errorf("no stop reported: %v", err)
	}
	if res.LogPath == "" {
		t.Error("a stopped run left no log")
	}
}

// TestFixStopsTheGroup stops a run whose agent started a process of its own:
// a tool left running is an orphan on the machine, and the group goes whole.
func TestFixStopsTheGroup(t *testing.T) {
	childPath := filepath.Join(t.TempDir(), "child")
	t.Setenv("FAKE_CHILD", childPath)
	fakeAgent(t, "claude", `#!/bin/sh
cat > "$FAKE_STDIN"
sleep 300 &
echo $! > "$FAKE_CHILD"
echo started
exec sleep 60
`)
	root := fixRepo(t)
	a, _ := agent.ByName("claude")
	lines := make(chan string, 4)
	done := make(chan struct{})
	stop := Start(context.Background(), FixRequest{Root: root, Agent: a, Task: "# Review\n"},
		func(line string) { lines <- line },
		func(FixResult, error) { close(done) })
	select {
	case <-lines:
	case <-time.After(10 * time.Second):
		t.Fatal("the run printed nothing")
	}
	pid := atoi(t, strings.TrimSpace(reading(t, childPath)))
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the tool the agent started is not running: %v", err)
	}
	stop()
	select {
	case <-done:
	case <-time.After(stopGrace + time.Second):
		t.Fatal("the run outlived its stop")
	}
	// The kill is the group's: the child of the agent goes too, and it is a
	// stray of the machine's no longer.
	deadline := time.Now().Add(time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the process group of the run survived it: %d", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("not a pid: %q", s)
	}
	return n
}

func TestFixUnsupportedAgent(t *testing.T) {
	fakeAgent(t, "opencode", fixingAgent)
	root := fixRepo(t)
	a, _ := agent.ByName("opencode")
	res, err, lines := runFix(t, FixRequest{Root: root, Agent: a, Task: "# Review\n"})
	if err == nil || !strings.Contains(err.Error(), "opencode") {
		t.Fatalf("opencode was not refused: %v", err)
	}
	if res.LogPath != "" || len(lines) != 0 {
		t.Errorf("opencode ran anyway: %q, %q", res.LogPath, lines)
	}
	// Nothing was written: a refused run leaves the review tree alone.
	if _, statErr := os.Stat(Path(root)); statErr == nil {
		t.Error("opencode wrote the task file")
	}
}

func TestFixNeedsATask(t *testing.T) {
	fakeAgent(t, "claude", fixingAgent)
	root := fixRepo(t)
	a, _ := agent.ByName("claude")
	if _, err, _ := runFix(t, FixRequest{Root: root, Agent: a, Task: "  \n"}); err == nil {
		t.Fatal("a run with no task reported no error")
	}
}

func TestFixCancelledContext(t *testing.T) {
	fakeAgent(t, "claude", `#!/bin/sh
cat > "$FAKE_STDIN"
sleep 60
`)
	root := fixRepo(t)
	a, _ := agent.ByName("claude")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	Start(ctx, FixRequest{Root: root, Agent: a, Task: "# Review\n"},
		nil,
		func(_ FixResult, err error) { done <- err })
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Errorf("no stop reported: %v", err)
		}
	case <-time.After(stopGrace + time.Second):
		t.Fatal("a cancelled context did not end the run")
	}
}

// piNotSignedInAgent fails the way Pi does when it is signed in to no model
// provider: the report a person running a repair can hit first.
const piNotSignedInAgent = `#!/bin/sh
cat > "$FAKE_STDIN"
cat >&2 <<'EOM'
No models available. Use /login to log into a provider via OAuth or API key. See:
/Users/ada/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/providers.md
EOM
exit 1
`

func TestFixExplainsAFailure(t *testing.T) {
	fakeAgent(t, "pi", piNotSignedInAgent)
	root := fixRepo(t)
	a, _ := agent.ByName("pi")
	res, err, _ := runFix(t, FixRequest{Root: root, Agent: a, Task: "# Review\n\n- `tracked.txt`: fix it\n"})
	if err == nil {
		t.Fatal("a run that exits 1 reported no error")
	}
	// The failure says what to do, with no path into an install in it.
	if !strings.Contains(res.ErrMessage, "Pi is not signed in to a model provider") {
		t.Errorf("message: %q", res.ErrMessage)
	}
	if strings.Contains(res.ErrMessage, "node_modules") {
		t.Errorf("the message carries a path of the install: %q", res.ErrMessage)
	}
	if !strings.Contains(err.Error(), "Pi is not signed in") {
		t.Errorf("error: %v", err)
	}
	// What the agent said is kept, and the log holds all of it.
	if !strings.Contains(res.ErrDetail, "No models available") {
		t.Errorf("detail: %q", res.ErrDetail)
	}
	if log := reading(t, res.LogPath); !strings.Contains(log, "node_modules") {
		t.Errorf("the log lost what the agent said:\n%s", log)
	}
}

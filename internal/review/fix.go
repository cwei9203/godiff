package review

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/godiff/internal/proc"
	"github.com/egoist/godiff/internal/shellenv"
)

// FixRequest is a repair of the review's comments.
type FixRequest struct {
	Root  string      // the repository root
	Agent agent.Agent // claude, codex or pi
	Model string      // "" for the agent's own default
	Task  string      // the rendered task file's text
}

// FixResult is what a repair did.
type FixResult struct {
	ExitCode int
	Duration time.Duration
	Changed  []string // paths as git status reports them, repo-relative
	LogPath  string   // where the whole output was written
	// ErrMessage is the failure as a sentence a person can act on, "" for a
	// run that failed in no way godiff knows.
	ErrMessage string
	ErrDetail  string // the tail of stderr as the agent wrote it, for the log and the detail
}

// stopGrace is how long a run may take to end once it is stopped, or its
// context is: the process group is killed outright, and this bounds the
// wait for the pipes it left.
const stopGrace = 3 * time.Second

// pipeWait bounds the wait for the pipes of a killed run, under stopGrace: a
// tool the agent started can hold the run's output open past the run itself,
// and the wait for it must still end inside a stop.
const pipeWait = 2 * time.Second

// errLines is how many lines of an agent's errors FixResult.ErrDetail keeps.
const errLines = 20

// taskDir is the directory godiff writes the task file in, at the root of
// the repository it reviews.
const taskDir = ".review"

// Start runs the repair in the repository. onLine gets each line of the
// agent's output as it comes; onDone gets the result once, at the end.
// stop ends the run; so does the context: the process group is killed, so
// that the tools the agent started go with it, and onDone follows within
// stopGrace.
//
// onLine runs on a goroutine of its own for the length of the run: a surface
// that draws from it must pass the line on to its own thread. onDone runs on
// that goroutine too, once, whether the run was stopped, failed or finished.
func Start(ctx context.Context, req FixRequest, onLine func(string), onDone func(FixResult, error)) (stop func()) {
	if onLine == nil {
		onLine = func(string) {}
	}
	if onDone == nil {
		onDone = func(FixResult, error) {}
	}
	ctx, cancel := context.WithCancel(ctx)
	started := time.Now()
	go func() {
		res, err := fix(ctx, req, started, onLine)
		onDone(res, err)
	}()
	return cancel
}

// fix writes the task file, runs the agent on it, and reads back what
// changed.
func fix(ctx context.Context, req FixRequest, started time.Time, onLine func(string)) (FixResult, error) {
	label := req.Agent.Label
	if label == "" {
		label = req.Agent.Name
	}
	if strings.TrimSpace(req.Task) == "" {
		return FixResult{}, errors.New("the review has no unresolved comment to fix")
	}
	bin, err := shellenv.LookPath(req.Agent.Name)
	if err != nil {
		return FixResult{}, fmt.Errorf("%s is not installed: %s was not found in your PATH", label, req.Agent.Name)
	}
	args, err := fixArgs(req)
	if err != nil {
		return FixResult{}, err
	}
	// The file and the prompt are one text: what the agent is told is
	// exactly what a user running it by hand would read.
	path := Path(req.Root)
	if err := WriteText(path, req.Task); err != nil {
		return FixResult{}, fmt.Errorf("could not write %s: %w", path, err)
	}
	logPath, logFile, err := createLog()
	if err != nil {
		return FixResult{}, err
	}
	defer logFile.Close()
	log := &syncWriter{w: logFile}
	fmt.Fprintf(log, "# %s %s\n# in %s\n# task %s\n", req.Agent.Name, strings.Join(args, " "), req.Root, path)

	cmd := exec.CommandContext(ctx, bin, args...)
	proc.HideConsole(cmd)
	proc.KillGroup(cmd)
	cmd.Dir = req.Root
	cmd.Env = shellenv.Environ()
	cmd.Stdin = strings.NewReader(req.Task)
	cmd.WaitDelay = pipeWait
	stderr := &stderrTail{log: log}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return FixResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return FixResult{}, fmt.Errorf("could not start %s: %w", label, err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		onLine(line)
		fmt.Fprintln(log, line)
	}
	waitErr := cmd.Wait()

	res := FixResult{ExitCode: exitCode(waitErr), Duration: time.Since(started), LogPath: logPath, ErrDetail: stderr.tail(errLines)}
	// What changed is git's answer, not the agent's own: a repair that
	// says it changed a file and did not is no repair.
	changed, err := changedPaths(req.Root)
	if err != nil {
		return res, fmt.Errorf("could not read the files changed: %w", err)
	}
	res.Changed = changed
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return res, fmt.Errorf("%s took too long and was stopped", label)
		}
		return res, fmt.Errorf("%s was stopped", label)
	}
	if waitErr != nil {
		res.ErrDetail = strings.TrimSpace(res.ErrDetail)
		// The failure as a sentence to act on, when it is one godiff knows;
		// what the agent said stays in ErrDetail, and in the log.
		detail := lastLine(res.ErrDetail)
		message, _ := agent.Explain(res.ErrDetail)
		switch {
		case message != "":
		case detail != "":
			message = detail
		default:
			message = waitErr.Error()
		}
		res.ErrMessage = message
		return res, fmt.Errorf("%s failed with exit code %d: %s", label, res.ExitCode, message)
	}
	// A run that worked has no error to explain: what it wrote to stderr is
	// progress, and the log holds it.
	res.ErrDetail = ""
	return res, nil
}

// fixArgs is the command of a repair, which is the command of an analysis
// with the permissions the other way round: an analysis reads the change,
// a repair writes it. The prompt goes on stdin, never in the arguments: a
// task file of forty comments is far past their limit.
func fixArgs(req FixRequest) ([]string, error) {
	switch req.Agent.Name {
	case "claude":
		// acceptEdits lets it edit and write the files. No --bare: a repair
		// has to follow the project's own CLAUDE.md and hooks, and no tool
		// whitelist: Edit and Write are the point.
		args := []string{"-p", "--permission-mode", "acceptEdits"}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		return args, nil
	case "codex":
		// workspace-write, where the analysis runs read-only: the change is
		// made in the work tree, and nowhere else.
		args := []string{"exec", "-s", "workspace-write", "--color", "never", "--skip-git-repo-check"}
		if req.Model != "" {
			args = append(args, "-m", req.Model)
		}
		// The prompt is read from stdin.
		return append(args, "-"), nil
	case "pi":
		// The default set of tools, which writes files and runs bash; the
		// analysis names the read-only ones instead.
		args := []string{"-p", "--no-session"}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		return args, nil
	case "opencode":
		// Its analysis adapter denies every tool that changes anything, and
		// the way to allow them is unverified: running it with the analysis
		// arguments would call itself a repair and change nothing.
		return nil, errors.New("opencode cannot repair: the way to let it write has not been verified, so it would run read-only and change nothing")
	}
	return nil, fmt.Errorf("unknown agent %q", req.Agent.Name)
}

// changedPaths lists the files git reports as changed in the repository. The
// task file is left out: .review/ is godiff's own write, not the agent's
// work, and counting it would report a change the agent never made.
func changedPaths(root string) ([]string, error) {
	repo, err := git.Open(root)
	if err != nil {
		return nil, err
	}
	// Unquoted: a path with an accent or a space is the path itself.
	out, err := repo.Git("-c", "core.quotePath=false", "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		// "XY path", or "XY old -> new" for a rename, which is the new
		// name now.
		path := line[3:]
		if i := strings.LastIndex(path, " -> "); i >= 0 {
			path = path[i+len(" -> "):]
		}
		path = unquote(strings.TrimSuffix(path, "/"))
		if path == "" || isTaskPath(path) {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// isTaskPath reports whether a path is godiff's own task file, or anything
// beside it.
func isTaskPath(path string) bool {
	return path == taskDir || strings.HasPrefix(path, taskDir+"/")
}

// unquote strips the quotes git puts around a path with an unusual byte in
// it, when it did not print it raw.
func unquote(path string) string {
	if !strings.HasPrefix(path, `"`) {
		return path
	}
	if s, err := strconv.Unquote(path); err == nil {
		return s
	}
	return strings.Trim(path, `"`)
}

// createLog opens the file a run's whole output goes to: one per run, named
// for when it started, in the user's home so that it outlives the window and
// any repository.
func createLog() (string, *os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, fmt.Errorf("could not find the home directory for the run log: %w", err)
	}
	dir := filepath.Join(home, ".godiff", "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("could not make %s: %w", dir, err)
	}
	path := filepath.Join(dir, "fix-"+time.Now().Format("20060102-150405.000")+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, fmt.Errorf("could not open %s: %w", path, err)
	}
	return path, f, nil
}

// exitCode is what a command exited with, -1 when a signal ended it, 0 when
// it did not fail.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// lastLine is the last line of a text that says something.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// syncWriter lets the stdout of a run and its stderr write one file without
// cutting into each other's lines.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// stderrTail keeps the end of what a command wrote to stderr, for the
// failure message, and the whole of it in the run's log.
type stderrTail struct {
	log io.Writer
	mu  sync.Mutex
	b   []byte
}

func (t *stderrTail) Write(p []byte) (int, error) {
	if t.log != nil {
		t.log.Write(p)
	}
	t.mu.Lock()
	t.b = append(t.b, p...)
	// A run of tests prints far more than a message needs.
	if len(t.b) > 16<<10 {
		t.b = t.b[len(t.b)-(8<<10):]
	}
	t.mu.Unlock()
	return len(p), nil
}

// tail is the last n lines of what it kept.
func (t *stderrTail) tail(n int) string {
	t.mu.Lock()
	s := string(t.b)
	t.mu.Unlock()
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

package main

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/review"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// fixLogLines is how many lines of a run's output the drawer keeps: a run of
// tests prints tens of thousands, and the whole of them go to the log file.
const fixLogLines = 10_000

// fixLogHeight is how much of the log the open drawer shows, in DIPs.
const fixLogHeight float32 = 220

// fixFailedLines is the most lines of an agent's errors the card shows.
const fixFailedLines = 6

// fixPhase is where a repair is: at work, or ended in one of three ways.
// Stopping one is no failure: the user asked for it.
type fixPhase uint8

const (
	fixRunning fixPhase = iota
	fixStopped
	fixSucceeded
	fixFailed
)

// fixEnd is how a run ended, as its result card shows it.
type fixEnd struct {
	phase    fixPhase
	exitCode int
	duration time.Duration
	changed  []string // the files git reports as changed
	logPath  string
	errText  string // the tail of the agent's errors, for a failure
}

// endFix reads the end of a repair. stopped is what the user asked for with
// the drawer's stop button, and is never a failure: a run killed on purpose
// reports what it was, and a run that failed shows the tail of what the agent
// wrote to stderr, or the error itself when it wrote nothing there.
func endFix(res review.FixResult, err error, stopped bool) fixEnd {
	end := fixEnd{
		phase:    fixSucceeded,
		exitCode: res.ExitCode,
		duration: res.Duration,
		changed:  res.Changed,
		logPath:  res.LogPath,
	}
	switch {
	case stopped:
		end.phase = fixStopped
	case err != nil:
		end.phase = fixFailed
		// The sentence to act on, when godiff knows the failure; else what
		// the agent wrote, else the error itself.
		end.errText = explainText(res.ErrReason, res.ErrDetail)
		if end.errText == "" {
			end.errText = strings.TrimSpace(err.Error())
		}
		end.errText = fixErrTail(end.errText, fixFailedLines)
	}
	return end
}

// fixErrTail is the last n lines of a text.
func fixErrTail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// logLine is a line of a run's output, numbered by the order it came in: the
// drawer keeps its place by the numbers as the oldest lines go.
type logLine struct {
	seq  int
	text string
}

// logRing keeps the last fixLogLines lines of a run, dropping the oldest.
type logRing struct {
	buf []logLine
	// at is where the next line goes once buf is full, and so where the
	// oldest line is.
	at  int
	seq int
}

// add keeps a line, dropping the oldest once there are too many.
func (r *logRing) add(text string) {
	r.seq++
	line := logLine{seq: r.seq, text: text}
	if len(r.buf) < fixLogLines {
		r.buf = append(r.buf, line)
		return
	}
	r.buf[r.at] = line
	r.at = (r.at + 1) % len(r.buf)
}

// lines is how many the ring keeps.
func (r *logRing) lines() int { return len(r.buf) }

// line is the i-th line from the oldest.
func (r *logRing) line(i int) logLine {
	if len(r.buf) < fixLogLines {
		return r.buf[i]
	}
	return r.buf[(r.at+i)%len(r.buf)]
}

// fixState is the drawer: the run it shows, the log kept of it, and what it
// ended with. It lives on the window rather than on the source shown, because
// it hosts a process: showing another change keeps the run, while a window on
// another repository starts with none of it.
type fixState struct {
	// open is whether the log shows; folded, the drawer is its one line.
	open bool

	phase    fixPhase
	agent    string // the agent's label, as the drawer names it
	comments int    // how many comments the task asked about
	started  time.Time
	duration time.Duration // of an ended run, as its result timed it
	exitCode int
	changed  []string
	logPath  string
	errText  string
	// stopped is set once the user asked to stop: the end that follows is
	// no failure.
	stopped bool

	ring logRing
	log  ui.ListState

	// stop ends the run, nil once it ended.
	stop func()

	// pending are lines written off the main thread, waiting for it, and
	// flushing is set while a flush is on its way: thousands of lines that
	// come together cross to the main thread once.
	mu       sync.Mutex
	pending  []string
	flushing bool
}

// elapsed is how long the run has gone, or went.
func (st *fixState) elapsed(now time.Time) time.Duration {
	if st.phase == fixRunning {
		return now.Sub(st.started)
	}
	return st.duration
}

// headline is the drawer's line: what the run does, or did, and for how long.
func (st *fixState) headline(now time.Time) string {
	who := st.agent
	if who == "" {
		who = msg().FixTheAgent()
	}
	took := fixDuration(st.elapsed(now))
	switch st.phase {
	case fixRunning:
		if st.stopped {
			return msg().FixHeadlineStopping(who)
		}
		return msg().FixHeadlineRunning(who, took)
	case fixStopped:
		return msg().FixHeadlineStopped(who, took)
	case fixFailed:
		return msg().FixHeadlineFailed(st.exitCode, took)
	}
	return msg().FixHeadlineFixed(msg().CommentsCount(st.comments), fixFiles(st.changed))
}

// fixDuration is a run's time, in the units a person reads: 0s, 2m13s.
func fixDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}

// fixFiles is how many files a run changed, or that it changed none.
func fixFiles(changed []string) string {
	if len(changed) == 0 {
		return msg().FixNoFileChanged()
	}
	return msg().FixFilesChanged(len(changed))
}

// fixAgent is the agent that repairs: the one the settings keep for repairs,
// else the one that reviews, else the first installed.
func (w *window) fixAgent() (agent.Agent, bool) {
	if a, ok := agent.ByName(w.settings.FixAgent); ok {
		return a, true
	}
	if a, ok := agent.ByName(w.settings.AIAgent); ok {
		return a, true
	}
	if installed, _ := installedAgents(); len(installed) > 0 {
		return installed[0], true
	}
	return agent.Agent{}, false
}

// fixModel is the model the repair runs with: the one the settings keep for
// repairs, else the reviewer's, else the agent's own default.
func (w *window) fixModel() string {
	if w.settings.FixModel != "" {
		return w.settings.FixModel
	}
	return w.settings.AIModel
}

// fixAtWork reports whether a repair is running.
func (w *window) fixAtWork() bool {
	return w.fix != nil && w.fix.phase == fixRunning
}

// fixHint names what a repair has to do and who does it, for the command bar.
func (w *window) fixHint() string {
	n := w.pendingComments()
	if n == 0 {
		return msg().NoUnresolvedComment()
	}
	a, ok := w.fixAgent()
	if !ok {
		return msg().NoCodingAgent()
	}
	return msg().CommentsCount(n) + " · " + a.Label
}

// openFixDialog offers the agent, its model and the scope of a repair.
func (w *window) openFixDialog() {
	if w.repo == nil || w.pendingComments() == 0 || w.fixAtWork() {
		return
	}
	a, ok := w.fixAgent()
	if !ok {
		return
	}
	w.fixChoice = fixChoice{open: true, agent: a.Name, model: w.fixModel(), scope: fixScopeAll}
}

// fixChoice is what a repair is asked before it starts: the agent, the model
// it runs with, and how much of the review it is asked about. It is the
// dialog's, not the settings': a repair asks differently once, as an agent
// chosen for one run.
type fixChoice struct {
	open  bool
	agent string // the command: claude, codex or pi
	model string // "" for the agent's own default
	scope fixScope
}

// fixDialog asks what to hand the agent before it starts.
func (w *window) fixDialog(c *ui.Context) {
	if !w.fixChoice.open {
		return
	}
	t := c.Theme()
	pick := &w.fixChoice
	installed, known := installedAgents()
	// The model follows the agent: another agent's models are not this one's.
	was := pick.agent
	start := func() {
		a, ok := agent.ByName(pick.agent)
		if !ok {
			return
		}
		pick.open = false
		w.startFix(a, pick.model, pick.scope)
	}
	ui.Modal(c, &pick.open, func() {
		ui.Column(c).Width(460).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, msg().FixReviewComments()).FontSize(16).Bold()
				ui.Text(c, msg().FixIntro(msg().UnresolvedCommentsCount(w.pendingComments()))).FontSize(13).TextColor(t.TextMuted)
			})
			ui.Field(c, msg().FixAgentField(), func() {
				ui.RadioGroup(c, func() {
					for _, a := range agent.All {
						missing := known && !slices.Contains(installed, a)
						label := a.Label
						if missing {
							label += msg().NotInstalledSuffix()
						}
						ui.Radio(c, &pick.agent, a.Name, label).Disabled(missing)
					}
				})
			})
			ui.Field(c, msg().FixModelField(), func() {
				w.fixModelButton(c, pick)
			})
			ui.Field(c, msg().FixRangeField(), func() {
				scope := pick.scope
				idx := 0
				for i, s := range fixScopeOrder {
					if s == scope {
						idx = i
					}
				}
				ui.Segmented(c, &idx, fixScopeLabels()...)
				pick.scope = fixScopeOrder[min(max(idx, 0), len(fixScopeOrder)-1)]
			})
			ui.Text(c, msg().FixWorkingTreeNote()).
				FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, msg().Cancel()).Clicked() {
					pick.open = false
				}
				if ui.PrimaryButton(c, msg().StartTheAgent()).Clicked() {
					start()
				}
			})
		})
	})
	if pick.agent != was {
		pick.model = ""
	}
}

// fixModelButton chooses the model the repair runs with, among those the
// agent lists, as the review's card does.
func (w *window) fixModelButton(c *ui.Context, pick *fixChoice) {
	a, ok := agent.ByName(pick.agent)
	if !ok {
		return
	}
	list := modelsOf(a, w.repo.Root)
	label := msg().DefaultModel()
	if pick.model != "" {
		label = modelName(pick.model, list.models)
	}
	ui.MenuButton(c, label, func(m *ui.Menu) {
		def := msg().DefaultModelOption()
		if list.def != "" {
			def += " (" + list.def + ")"
		}
		if m.Item(def).Checked(pick.model == "").Chosen() {
			pick.model = ""
		}
		m.Separator()
		switch {
		case !list.done:
			m.Item(msg().LoadingModels()).Disabled(true)
			return
		case list.err != nil:
			msg, _, _ := strings.Cut(strings.TrimSpace(errorText(list.err)), "\n")
			if len(msg) > 80 {
				msg = msg[:80] + "…"
			}
			m.Item(msg).Disabled(true)
			return
		}
		modelItems(m, list.models, func(m *ui.Menu, label, id string) {
			if m.Item(label).Checked(pick.model == id).Chosen() {
				pick.model = id
			}
		})
	}).Height(28).Tooltip(msg().ModelThatFixes(cmp.Or(pick.model, list.def, msg().AgentDefault(a.Label))))
}

// startFix hands the review's comments to an agent, in the working tree, and
// shows what it does in the drawer as it comes.
func (w *window) startFix(a agent.Agent, model string, scope fixScope) {
	if w.repo == nil {
		return
	}
	doc := w.reviewDocumentFor(scope)
	task := review.Render(doc)
	if strings.TrimSpace(task) == "" {
		mygo.Dialog.Error(msg().NothingToFix(), msg().NothingToFixDetail())
		return
	}
	st := &fixState{open: true, phase: fixRunning, agent: a.Label, comments: len(doc.Comments), started: time.Now()}
	w.fix = st
	st.log.ScrollToEnd()
	st.stop = review.Start(context.Background(), review.FixRequest{Root: w.repo.Root, Agent: a, Model: model, Task: task},
		func(line string) { w.fixLine(st, line) },
		func(res review.FixResult, err error) {
			w.update(func() { w.fixDone(st, res, err) })
		})
	w.invalidate()
}

// fixLine takes a line of the agent's output, written on the goroutine of the
// run, to the main thread: the lines that arrive in the meantime go with it.
func (w *window) fixLine(st *fixState, line string) {
	st.mu.Lock()
	st.pending = append(st.pending, line)
	flush := !st.flushing
	st.flushing = true
	st.mu.Unlock()
	if !flush {
		return
	}
	w.update(func() {
		st.mu.Lock()
		lines := st.pending
		st.pending, st.flushing = nil, false
		st.mu.Unlock()
		for _, l := range lines {
			st.ring.add(l)
		}
	})
}

// fixDone shows how the run ended: the drawer's one line becomes the result,
// and nothing is refreshed for the user: what the agent did is theirs to look
// at, and to take with a refresh of their own.
func (w *window) fixDone(st *fixState, res review.FixResult, err error) {
	if w.fix != st {
		return
	}
	end := endFix(res, err, st.stopped)
	st.phase, st.exitCode, st.duration = end.phase, end.exitCode, end.duration
	st.changed, st.logPath, st.errText = end.changed, end.logPath, end.errText
	st.stop = nil
}

// stopFix stops the run: the backend kills its process group, so that the
// tools the agent started go with it, and the end that follows shows as
// stopped rather than as a failure.
func (w *window) stopFix() {
	st := w.fix
	if st == nil || st.phase != fixRunning || st.stopped {
		return
	}
	st.stopped = true
	if st.stop != nil {
		st.stop()
	}
	w.invalidate()
}

// openFixLog opens the run's whole log in the way the system opens a file.
func openFixLog(path string) {
	if path == "" {
		return
	}
	if err := mygo.Shell.OpenPath(path); err != nil {
		mygo.Dialog.Error(msg().CouldNotOpenLog(), err.Error())
	}
}

// fixDrawer shows the repair along the bottom of the review: one line while
// it is folded, the agent's output below it open, and its result once it
// ended.
func (w *window) fixDrawer(c *ui.Context, pal *palette) {
	st := w.fix
	if st == nil {
		return
	}
	ui.Column(c).Shrink(0).Background(pal.surface).BorderWidth(1, 0, 0, 0).BorderColor(pal.border).Children(func() {
		ui.Column(c).Padding(7, 12).Gap(5).Children(func() {
			w.fixHeader(c, st)
			// Folded, the drawer is the one line of the header; open, the
			// result of a run that ended sits above its log.
			if st.open && st.phase != fixRunning {
				w.fixResult(c, st)
			}
		})
		if st.open {
			w.fixLog(c, pal, st)
		}
	})
	if st.phase == fixRunning {
		// The clock of the folded line: a frame a second, as the review's
		// card does it, rather than a goroutine of its own.
		c.After(time.Second)
	}
}

// fixHeader is the drawer's first row: what the run does, the stop button
// while it runs, and the arrow that folds the log away.
func (w *window) fixHeader(c *ui.Context, st *fixState) {
	t := c.Theme()
	svg, tip := iconChevronDown, msg().HideOutput()
	if !st.open {
		svg, tip = iconChevronRight, msg().ShowOutput()
	}
	ui.Row(c).Gap(8).MinHeight(24).Children(func() {
		if iconButton(c, svg, tip).Size(24, 24).Clicked() {
			st.open = !st.open
		}
		switch st.phase {
		case fixRunning:
			ui.Spinner(c).Size(13, 13)
		case fixSucceeded:
			ui.Icon(c, iconCheckCircle).FontSize(13).TextColor(t.Accent)
		case fixStopped:
			ui.Icon(c, iconStop).FontSize(13).TextColor(t.TextMuted)
		default:
			ui.Icon(c, iconAlert).FontSize(13).TextColor(t.Danger)
		}
		ui.Text(c, st.headline(w.now)).FontSize(12).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
		ui.Spacer(c)
		if st.phase == fixRunning {
			b := ui.Button(c, "").Height(24)
			b.Children(func() {
				ui.Icon(c, iconStop).FontSize(11)
				ui.Text(c, textStop()).FontSize(12)
			})
			if b.Disabled(st.stopped).Clicked() {
				w.stopFix()
			}
			return
		}
		if iconButton(c, iconClose, msg().CloseDrawer()).Size(24, 24).Clicked() {
			w.fix = nil
		}
	})
}

// fixResult shows how a run ended: its exit code, how long it took, the files
// git reports as changed, and what to do with them.
func (w *window) fixResult(c *ui.Context, st *fixState) {
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(0, 0, 0, 28).Children(func() {
		if st.phase != fixStopped {
			ui.Text(c, msg().ExitCode(st.exitCode)).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		}
		ui.Text(c, msg().Took(fixDuration(st.duration))).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		// The files are git's answer, not the agent's own.
		files := msg().NoFilesChanged()
		if len(st.changed) > 0 {
			files = strings.Join(st.changed, ", ")
		}
		ui.Text(c, files).Font(w.codeFont()).FontSize(11).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0).
			Tooltip(fixFiles(st.changed))
	})
	if st.errText != "" {
		ui.Text(c, st.errText).Font(w.codeFont()).FontSize(11).TextColor(t.Danger).Selectable().MaxLines(fixFailedLines).
			Padding(0, 0, 0, 28)
	}
	ui.Row(c).Gap(8).Padding(0, 0, 0, 28).Children(func() {
		log := ui.Button(c, msg().OpenLogFile()).Height(24)
		if log.Disabled(st.logPath == "").Clicked() {
			openFixLog(st.logPath)
		}
		if ui.Button(c, msg().RefreshDiff()).Height(24).Clicked() {
			w.refresh()
		}
	})
}

// fixLog is the agent's output, in one font, following its end unless the
// user scrolled away from it.
func (w *window) fixLog(c *ui.Context, pal *palette, st *fixState) {
	n := st.ring.lines()
	st.log.Key = func(i int) any { return st.ring.line(i).seq }
	st.log.FollowEnd = true
	log := ui.List(c, &st.log, n, func(i int) {
		ui.Text(c, st.ring.line(i).text).Font(w.codeFont()).FontSize(11).MinHeight(16).SingleLine().Padding(0, 12)
	}).Height(fixLogHeight).Background(pal.codeBg).Focusable().Label(msg().AgentOutput()).Children(func() {
		if n == 0 {
			emptyState(c, pal, emptyBox{shape: emptySmall, icon: iconCommand, title: msg().WaitingForOutput()})
		}
	})
	// The drawer is a place the keys go, like the review and the tree.
	focusRing(log, pal, 0)
}

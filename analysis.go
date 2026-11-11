package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// analysisState is an agent's review of a source: the one it gave, or the
// one under way.
type analysisState struct {
	result *agent.Analysis
	// fingerprints are the files' as the agent saw them, to tell which
	// changed since.
	fingerprints map[string]string
	running      bool
	progress     agent.Progress
	started      time.Time
	// err is what the card says of a failure: a sentence to act on, the
	// agent's own words only when they are all there is. errDetail is those
	// words, kept aside, and errOpen shows them.
	err       string
	errDetail string
	errOpen   bool
	cancel    context.CancelFunc
	agent     string // the label of the agent at work
	// allNotes lists every note in the card, not the first alone.
	allNotes bool
	// offered is set when the user chose to group by a review there is
	// not yet: the card offers one, which runs once they ask.
	offered bool
	// checked is set once a review saved was looked for.
	checked bool
}

// runAgent runs an agent; tests replace it.
var runAgent = agent.Run

func (w *window) analysisFor(src source) *analysisState {
	if w.analyses == nil {
		w.analyses = map[source]*analysisState{}
	}
	st := w.analyses[src]
	if st == nil {
		st = &analysisState{}
		w.analyses[src] = st
	}
	return st
}

// analysisResult is the agent's review of the source shown, nil for none.
func (w *window) analysisResult() *agent.Analysis {
	if st := w.analyses[w.source]; st != nil {
		return st.result
	}
	return nil
}

// showSummary reports whether the surface shows the agent's review: once
// there is one, one under way, one that failed, or one offered.
func (w *window) showSummary() bool {
	st := w.analyses[w.source]
	if st == nil {
		return false
	}
	// A review whose overall summary is turned off leaves no card rather
	// than an empty one; one under way, failed or offered still holds what the
	// card is there for.
	if !w.settings.OverallSummary && st.result != nil && !st.running && st.err == "" && !st.offered {
		return false
	}
	return st.result != nil || st.running || st.err != "" || st.offered
}

// thinkingStart is the step the card shows before the agent's first: it is
// started before the agent is found.
var thinkingStart = agent.Progress{Kind: agent.ProgressThinking, Detail: "Starting…"}

// analysisKeyInput is everything an analysis is made of. Change any of it,
// and the analysis of it is another analysis: another prompt or contract, as
// another model, in another language, or of other files.
type analysisKeyInput struct {
	SchemaVersion int
	PromptVersion string
	Agent         string // the command, claude or codex
	Model         string // "" for the agent's own default
	Locale        string // BCP 47, "" for the agent's own
	Root          string // the repository
	Source        string // the change, as kind:ref
	Fingerprints  map[string]string
}

// analysisKey identifies an analysis, as a hash of what made it. The files
// enter it by path and content fingerprint, sorted, so that neither the
// order nor the shape of the diff changes it. What is left out — the agent's
// version, its login — does not change the review: a new CLI is no reason to
// throw away an analysis already paid for.
func analysisKey(in analysisKeyInput) string {
	h := sha256.New()
	field := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	field(strconv.Itoa(in.SchemaVersion))
	field(in.PromptVersion)
	field(in.Agent + "/" + cmp.Or(in.Model, "default"))
	field(in.Locale)
	field(in.Root)
	field(in.Source)
	paths := make([]string, 0, len(in.Fingerprints))
	for p := range in.Fingerprints {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		field(p)
		field(in.Fingerprints[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// analysisKeyOf keys an analysis as it stands now: what an agent and model
// the settings chose would make would be keyed so. An analysis keyed to
// anything else is another analysis, not this one.
func (w *window) analysisKeyOf(src source, a *agent.Analysis, fingerprints map[string]string) string {
	return analysisKey(analysisKeyInput{
		SchemaVersion: agent.SchemaVersion,
		PromptVersion: agent.PromptVersion,
		// A setting left automatic takes the agent and model the analysis
		// was made with: those are the ones it chose.
		Agent:        cmp.Or(w.settings.AIAgent, a.Agent),
		Model:        cmp.Or(w.settings.AIModel, a.Model),
		Locale:       cmp.Or(analysisLocale(), a.Locale),
		Root:         w.repo.Root,
		Source:       strconv.Itoa(int(src.kind)) + ":" + src.ref,
		Fingerprints: fingerprints,
	})
}

// readerLocale is the language the reader prefers, as the system has them,
// read once when the app is ready: asking MyGo for them runs on its main
// loop, which a test has not.
var readerLocale string

// analysisLocale is the language a review is written in: the one the
// settings name, else the first the reader prefers, as macOS puts them,
// else the shell's.
func analysisLocale() string {
	if tag := localeTag(cfg.Get().SummaryLocale); tag != "" {
		return tag
	}
	if tag := localeTag(readerLocale); tag != "" {
		return tag
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if tag := localeTag(os.Getenv(key)); tag != "" {
			return tag
		}
	}
	return ""
}

// localeTag reads a language as BCP 47: the tag alone, its encoding and
// its modifiers left out, its parts joined by dashes.
func localeTag(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "_", "-")
	if s == "C" || s == "POSIX" {
		return ""
	}
	return s
}

// analysisSlot names the place the review of a source is saved: one per
// source, so that it is found before the diff is loaded, with its key
// inside to tell whether it is the review that would run now.
func analysisSlot(root string, src source) string {
	sum := sha256.Sum256([]byte(root + "\x00" + strconv.Itoa(int(src.kind)) + ":" + src.ref))
	return hex.EncodeToString(sum[:])
}

// analysesDir is where the reviews of every source are saved, side by side,
// "" without a place for them, as in tests. The sweep of the cache keeps
// them per repository, and deletes the ones no window needs again.
func analysesDir() string {
	if state.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(state.path), "analyses")
}

// analysisPath is where the review of a source is saved, "" without a
// place for it, as in tests.
func analysisPath(root string, src source) string {
	dir := analysesDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, analysisSlot(root, src)+".json")
}

type savedAnalysis struct {
	Analysis *agent.Analysis `json:"analysis"`
	// Key is the analysisKey of what made the analysis: one saved under
	// another key — by another prompt, model or language, or by a version
	// before keys — is no review of what would run now.
	Key string `json:"key,omitempty"`
	// Root is the repository the review is of: the reviews of every
	// repository are saved in one directory, and the sweep keeps each
	// repository's own.
	Root         string            `json:"root,omitempty"`
	Fingerprints map[string]string `json:"fingerprints"`
}

// loadAnalysis reads the review saved of a source, once.
func (w *window) loadAnalysis(src source) {
	st := w.analysisFor(src)
	if st.checked {
		return
	}
	st.checked = true
	path := analysisPath(w.repo.Root, src)
	if path == "" {
		return
	}
	w.background(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var saved savedAnalysis
		if json.Unmarshal(data, &saved) != nil || saved.Analysis == nil || len(saved.Analysis.Groups) == 0 {
			return
		}
		a := saved.Analysis
		// An analysis of another contract, or one keyed as neither this
		// prompt, this agent, this model, this language nor these files
		// would be, is not a review of what runs now. It is no review at
		// all rather than a stale one, and the next run writes over it.
		if a.SchemaVersion != agent.SchemaVersion || saved.Key == "" || saved.Key != w.analysisKeyOf(src, a, saved.Fingerprints) {
			return
		}
		a.Normalize()
		w.update(func() {
			if st.result != nil || st.running {
				return
			}
			st.result, st.fingerprints = a, saved.Fingerprints
			if w.source == src {
				w.regroupSoon()
			}
		})
	})
}

// saveAnalysis writes an analysis where its source's review is kept, with
// its key and its repository, so that it is read only while it is the review
// that would run.
func saveAnalysis(path, root, key string, a *agent.Analysis, fingerprints map[string]string) {
	if path == "" {
		return
	}
	a.Normalize()
	data, err := json.Marshal(savedAnalysis{Analysis: a, Key: key, Root: root, Fingerprints: fingerprints})
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, data, 0o644)
}

// runAnalysis asks the agent of the settings, else the first installed, to
// review the changes shown.
func (w *window) runAnalysis() {
	src := w.source
	st := w.analysisFor(src)
	if st.running || len(w.files) == 0 || w.loading {
		return
	}
	req := agent.Request{Dir: w.repo.Root, Model: w.settings.AIModel}
	var base, head string
	switch src.kind {
	case sourceWorkingTree:
		req.Title, req.Checkout = "Uncommitted changes", true
		if w.branch != "" {
			req.Title += " on " + w.branch
		}
	case sourceBranch:
		req.Title, req.Checkout = fmt.Sprintf("The changes of %s since it branched from %s", w.branch, src.ref), true
		base, head = w.base, "HEAD"
	case sourceCommit:
		if w.commit != nil {
			req.Title, req.Description = w.commit.Subject, w.commit.Body
		}
		req.Rev = src.ref
	case sourcePull:
		if w.pr == nil || w.pr.meta == nil {
			return
		}
		m := w.pr.meta
		req.Title, req.Description, req.URL, req.Rev = m.Title, plainBody(m.Body), m.HTMLURL, w.pr.head
		base, head = w.base, w.pr.head
	}
	files := slices.Clone(w.files)
	fingerprints := map[string]string{}
	for _, f := range files {
		fingerprints[f.Path] = f.Fingerprint
	}
	name := w.settings.AIAgent
	path := analysisPath(w.repo.Root, src)
	ctx, cancel := context.WithCancel(context.Background())
	st.running, st.err, st.errDetail, st.errOpen, st.started, st.cancel, st.offered = true, "", "", false, time.Now(), cancel, false
	st.progress = thinkingStart
	st.agent = ""
	req.Locale = analysisLocale()
	w.rowsDirty = true
	w.list.ScrollTo(0, ui.Start)
	w.background(func() {
		defer cancel()
		a, ok := agent.ByName(name)
		if !ok {
			installed := agent.Installed()
			if len(installed) == 0 {
				w.update(func() {
					st.running = false
					st.err = "No coding agent was found. Install Claude Code, Codex, OpenCode or Pi, or name one as aiAgent in the config file."
					w.rowsDirty = true
				})
				return
			}
			a = installed[0]
		}
		w.update(func() { st.agent = a.Label })
		// The files' descriptions read no state the main thread changes.
		req.Files = analysisFiles(files)
		if base != "" && head != "" {
			req.Commits = w.repo.Subjects(base, head, 100)
		}
		last := agent.Progress{}
		an, err := runAgent(ctx, a, req, func(p agent.Progress) {
			if p.Kind == last.Kind && p.Detail == last.Detail && slices.Equal(p.Paths, last.Paths) {
				return
			}
			last = p
			w.update(func() { st.progress = p })
		})
		if err == nil {
			// The revision the review covers: a new commit is a review of
			// a change that has moved on.
			rev := head
			switch {
			case rev != "":
			case src.kind == sourceCommit:
				rev = src.ref
			default:
				rev = "HEAD"
			}
			if sha, rerr := w.repo.Resolve(rev); rerr == nil {
				an.HeadSHA = sha
			}
			saveAnalysis(path, w.repo.Root, w.analysisKeyOf(src, an, fingerprints), an, fingerprints)
		}
		w.update(func() {
			st.running, st.cancel = false, nil
			switch {
			case errors.Is(err, context.Canceled):
				st.err = ""
			case err != nil:
				// The failure as a sentence to act on, with what the agent
				// said kept under it: one godiff does not know is shown as
				// it came.
				st.err, st.errDetail = agent.Explain(err.Error())
				st.errOpen = false
				if st.err == "" {
					st.err = err.Error()
				}
			default:
				st.result, st.fingerprints, st.err, st.errDetail = an, fingerprints, "", ""
			}
			if w.source == src {
				w.regroupSoon()
			}
		})
	})
}

// stopAnalysis stops the agent at work on the source shown.
func (w *window) stopAnalysis() {
	if st := w.analyses[w.source]; st != nil && st.cancel != nil {
		st.cancel()
	}
}

// changedSince counts the files that changed since the agent's review: new,
// gone or different.
func (w *window) changedSince(st *analysisState) int {
	n := 0
	seen := map[string]bool{}
	for _, f := range w.files {
		seen[f.Path] = true
		if fp, ok := st.fingerprints[f.Path]; !ok || fp != f.Fingerprint {
			n++
		}
	}
	for p := range st.fingerprints {
		if !seen[p] {
			n++
		}
	}
	return n
}

// summaryCard shows the agent's review of the changes, in one of its three
// states: offered and not started, at work, or the review it gave — or the
// failure that ended it.
//
// The three have one head, one padding and one rhythm, so that the card holds
// still as the review moves from one state to the next: the head says where the
// review stands and holds the one action of the state, and the body says the
// rest.
func (w *window) summaryCard(c *ui.Context, pal *palette) ui.Element {
	t := c.Theme()
	st := w.analyses[w.source]
	offered := st.result == nil && !st.running && st.err == ""
	return ui.Column(c).Padding(padCard, 16).Gap(gapCard).Radius(radiusCard).
		Background(pal.surface).Border(1, t.Accent.Alpha(0.3)).Children(func() {
		ui.Row(c).Height(lineCardHead).Gap(8).Children(func() {
			ui.Icon(c, iconSparkle).FontSize(15).TextColor(t.Accent)
			ui.Text(c, textAIReview()).FontSize(14).Bold().Shrink(0)
			if mark := w.reviewMark(st); mark != "" {
				ui.Text(c, mark).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
			}
			ui.Spacer(c)
			switch {
			case st.running:
				// The one action of a review at work: stop it.
				b := ui.Button(c, "").Height(26).Children(func() {
					ui.Icon(c, iconStop).FontSize(12)
					ui.Text(c, textStop).FontSize(12)
				})
				if b.Clicked() {
					w.stopAnalysis()
				}
			case offered:
				if iconButton(c, iconClose, msg().Dismiss()).Size(26, 26).Clicked() {
					st.offered = false
					if w.groupPref == groupAI {
						w.groupPref = groupAuto
					}
					w.regroupSoon()
				}
			default:
				if st.result != nil {
					// The summary shows whole, or else as its first line alone.
					svg, tip := iconChevronDown, "Hide the summary"
					if !w.settings.SummaryExpanded {
						svg, tip = iconChevronRight, "Show the summary"
					}
					if iconButton(c, svg, tip).Size(26, 26).Clicked() {
						cfg.Update(func(s *Settings) { s.SummaryExpanded = !s.SummaryExpanded })
					}
				}
				label := textTryAgain
				if st.result != nil {
					label = textReviewAgain
				}
				if ui.Button(c, label).Height(26).Clicked() {
					w.runAnalysis()
				}
			}
		})
		switch {
		case st.running:
			w.cardRunning(c, pal, st)
		case offered:
			w.cardOffered(c, pal)
		}
		if st.err != "" {
			ui.Text(c, st.err).FontSize(12).TextColor(t.Danger).Selectable().MaxLines(6)
			if d := st.errDetail; d != st.err {
				// What the agent said itself, for a report or a bug: it is
				// no sentence to act on, so it stays folded away.
				label := "Show what it said"
				if st.errOpen {
					label = "Hide what it said"
				}
				if ui.ButtonBase(c).Padding(2, 0).Children(func() {
					ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted)
				}).Clicked() {
					st.errOpen = !st.errOpen
				}
				if st.errOpen {
					ui.Text(c, d).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).Selectable().MaxLines(12)
				}
			}
		}
		a := st.result
		if a == nil {
			return
		}
		summary := strings.TrimSpace(a.OverallSummary)
		if w.settings.OverallSummary && summary != "" {
			if w.settings.SummaryExpanded {
				ui.Text(c, summary).FontSize(13).Selectable()
			} else {
				// One line of it, which opens the whole.
				line, _, _ := strings.Cut(summary, "\n")
				b := ui.ButtonBase(c).Padding(2, 6).Radius(6).Gap(6).Label("Show the summary").
					Tooltip("Show the whole summary of the review")
				if b.Hovered() {
					b.Background(pal.hover)
				}
				if b.Clicked() {
					cfg.Update(func(s *Settings) { s.SummaryExpanded = true })
				}
				b.Children(func() {
					ui.Text(c, line).FontSize(13).TextColor(t.Text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					ui.Icon(c, iconChevronRight).FontSize(12).TextColor(t.TextMuted).Shrink(0)
				})
			}
		}
		notes := 0
		for _, g := range a.Groups {
			notes += len(g.FileNotes) + len(g.LineNotes)
		}
		ui.Row(c).Gap(10).Children(func() {
			ui.Textf(c, "%s · %s", plural(len(a.Groups), "group"), plural(notes, "note")).FontSize(12).TextColor(t.TextMuted)
			if n := a.Critical(); n > 0 {
				ui.Row(c).Gap(4).Children(func() {
					ui.Icon(c, iconAlert).FontSize(12).TextColor(careTone(pal))
					ui.Textf(c, "%d to take care with", n).FontSize(12).FontWeight(600).TextColor(careTone(pal))
				})
			}
			if n := w.changedSince(st); n > 0 {
				ui.Textf(c, "%s changed since", plural(n, "file")).FontSize(12).TextColor(pal.ref).
					Tooltip("Notes on the files changed since do not show. Review again to include them.")
			}
			ui.Spacer(c)
			if w.grouping() != groupAI {
				b := ui.ButtonBase(c).Children(func() {
					ui.Text(c, "Group files by this review").FontSize(12).FontWeight(600).TextColor(t.Accent)
				})
				if b.Clicked() {
					w.setGrouping(groupAI)
				}
			}
		})
		w.noteList(c, pal, st)
	})
}

// The card's own measures, on the steps of the review: its head, its shortest
// body, and the gap between them.
const (
	lineCardHead = 28
	cardBodyMin  = 44
	gapCard      = 8
)

// reviewMark says where the review comes from, in the card's head: the agent,
// its model and when it ran; or where the review stands, while it is not one.
func (w *window) reviewMark(st *analysisState) string {
	switch {
	case st.running:
		return st.agent
	case st.err != "":
		return textReviewFailed
	case st.result == nil:
		return textNotRun()
	}
	a := st.result
	by := a.Agent
	if ag, ok := agent.ByName(a.Agent); ok {
		by = ag.Label
	}
	if a.Model != "" {
		by += " · " + a.Model
	}
	if since := relativeTime(w.now, a.GeneratedAt); since != "" {
		by += " · " + since
	}
	return by
}

// cardOffered is the card before the review runs: what an agent would do, and
// the choice of the agent and the model beneath it, as the second thing the
// card says rather than the first.
func (w *window) cardOffered(c *ui.Context, pal *palette) {
	t := c.Theme()
	ui.Column(c).Gap(gapCard).MinHeight(cardBodyMin).Children(func() {
		ui.Text(c, textOffered()).FontSize(13).TextColor(t.TextMuted)
		ui.Row(c).Gap(6).Children(func() {
			w.agentMenu(c)
			w.modelMenu(c)
			ui.Spacer(c)
			if ui.PrimaryButton(c, textReviewWithAI()).Height(26).Clicked() {
				w.runAnalysis()
			}
		})
	})
}

// cardRunning is the card while the agent works: the three steps of a review
// and where it stands among them, what it does now, and how long it has taken.
func (w *window) cardRunning(c *ui.Context, pal *palette, st *analysisState) {
	t := c.Theme()
	steps := []struct {
		kind  agent.ProgressKind
		label string
	}{
		{agent.ProgressThinking, textThinking},
		{agent.ProgressReading, textReading},
		{agent.ProgressOrganizing, textOrganizing},
	}
	at := slices.IndexFunc(steps, func(s struct {
		kind  agent.ProgressKind
		label string
	}) bool {
		return s.kind == st.progress.Kind
	})
	if at < 0 {
		at = 0
	}
	ui.Column(c).Gap(gapCard).MinHeight(cardBodyMin).Children(func() {
		// The steps, in the order a review takes them: done, now, waiting.
		ui.Row(c).Gap(14).Children(func() {
			for i, step := range steps {
				row := ui.Row(c).Gap(5).Shrink(0)
				switch {
				case i < at:
					row.Children(func() {
						ui.Icon(c, iconCheck).FontSize(11).TextColor(pal.viewed).Shrink(0)
						ui.Text(c, step.label).FontSize(11).TextColor(t.TextMuted).Shrink(0)
					})
				case i == at:
					row.Children(func() {
						ui.Icon(c, iconSparkle).FontSize(11).TextColor(t.Accent).Shrink(0)
						ui.Text(c, step.label).FontSize(11).FontWeight(600).TextColor(t.Text).Shrink(0)
					})
				default:
					row.Children(func() {
						ui.Box(c).Size(5, 5).Radius(3).Background(t.TextMuted.Alpha(0.4)).AlignSelf(ui.Center).Shrink(0)
						ui.Text(c, step.label).FontSize(11).TextColor(t.TextMuted).Shrink(0)
					})
				}
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c).Size(13, 13)
			who := st.agent
			ui.Text(c, progressLine(st.progress)).FontSize(12).TextColor(t.TextMuted).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if who != "" {
				ui.Text(c, who).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			}
			elapsed := w.now.Sub(st.started).Truncate(time.Second)
			ui.Text(c, elapsed.String()).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			c.After(time.Second)
		})
	})
}

// progressLine is the step the agent is on, in the review's own words: the
// sentence moves with the kind, rather than standing still through the run.
func progressLine(p agent.Progress) string {
	switch p.Kind {
	case agent.ProgressThinking:
		return textThinking
	case agent.ProgressReading:
		if n := len(p.Paths); n > 0 {
			return "Reading " + plural(n, "file")
		}
		return textReading
	case agent.ProgressOrganizing:
		return textOrganizing
	}
	return textStarting
}

// listedNotes is how many notes the card lists before it is asked for all.
const listedNotes = 5

// noteList lists the notes of the agent's review, each going to its line.
func (w *window) noteList(c *ui.Context, pal *palette, st *analysisState) {
	t := c.Theme()
	notes := w.orderedNotes()
	if len(notes) == 0 {
		return
	}
	shown := notes
	if !st.allNotes && len(notes) > listedNotes {
		shown = notes[:listedNotes]
	}
	ui.Column(c).Gap(1).Margin(0, -8).Children(func() {
		for _, n := range shown {
			where := filepath.Base(n.path)
			switch {
			case n.file:
			case n.side == sideOld:
				where += fmt.Sprintf(":%d (old)", n.line)
			default:
				where += fmt.Sprintf(":%d", n.line)
			}
			accent, icon := t.Accent, iconSparkle
			if n.critical {
				accent, icon = careTone(pal), iconAlert
			}
			b := ui.ButtonBase(c).Padding(4, 8).Radius(6).Label("Go to the note on " + where).Tooltip(n.path)
			if b.Hovered() || n == w.noteSel {
				b.Background(pal.hover)
			}
			b.Children(func() {
				ui.Row(c).Gap(8).Grow(1).MinWidth(0).Children(func() {
					ui.Icon(c, icon).FontSize(12).TextColor(accent).Shrink(0)
					ui.Text(c, where).Font(w.codeFont()).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(0).MaxWidth(260)
					ui.Text(c, n.text).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				})
			})
			if b.Clicked() {
				w.revealNote(n)
			}
		}
		if len(notes) > listedNotes {
			label := fmt.Sprintf("Show %d more", len(notes)-listedNotes)
			if st.allNotes {
				label = "Show fewer"
			}
			b := ui.ButtonBase(c).Padding(4, 8).Children(func() {
				ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.Accent)
			})
			if b.Clicked() {
				st.allNotes = !st.allNotes
			}
		}
	})
}

// agentMenu chooses the agent that reviews.
func (w *window) agentMenu(c *ui.Context) {
	installed, known := installedAgents()
	label := "Automatic"
	if a, ok := agent.ByName(w.settings.AIAgent); ok {
		label = a.Label
	}
	ui.MenuButton(c, label, func(m *ui.Menu) {
		if m.Item("Automatic").Checked(w.settings.AIAgent == "").Chosen() {
			cfg.Update(func(s *Settings) { s.AIAgent = "" })
		}
		m.Separator()
		for _, a := range agent.All {
			missing := known && !slices.Contains(installed, a)
			item := a.Label
			if missing {
				item += " (not installed)"
			}
			if m.Item(item).Checked(w.settings.AIAgent == a.Name).Disabled(missing).Chosen() {
				name := a.Name
				cfg.Update(func(s *Settings) { s.AIAgent, s.AIModel = name, "" })
			}
		}
	}).Height(26).Tooltip("The agent that reviews")
}

// reviewAgent is the agent that reviews: the settings', else the first
// installed, once known.
func (w *window) reviewAgent() (agent.Agent, bool) {
	if a, ok := agent.ByName(w.settings.AIAgent); ok {
		return a, true
	}
	if installed, _ := installedAgents(); len(installed) > 0 {
		return installed[0], true
	}
	return agent.Agent{}, false
}

// modelMenu chooses the model the agent reviews with, among those it
// lists.
func (w *window) modelMenu(c *ui.Context) {
	a, ok := w.reviewAgent()
	if !ok {
		return
	}
	list := modelsOf(a, w.repo.Root)
	chosen := w.settings.AIModel
	label := "Default Model"
	if chosen != "" {
		label = modelName(chosen, list.models)
	}
	setModel := func(id string) { cfg.Update(func(s *Settings) { s.AIModel = id }) }
	ui.MenuButton(c, label, func(m *ui.Menu) {
		def := "Default"
		if list.def != "" {
			def += " (" + list.def + ")"
		}
		if m.Item(def).Checked(chosen == "").Chosen() {
			setModel("")
		}
		m.Separator()
		if chosen != "" && !slices.ContainsFunc(list.models, func(md agent.Model) bool { return md.ID == chosen }) {
			// Named in the config file.
			m.Item(chosen).Checked(true)
		}
		switch {
		case !list.done:
			m.Item("Loading models…").Disabled(true)
			return
		case list.err != nil:
			// The failure in a sentence to act on, the agent's own words
			// only when godiff knows this one not.
			raw := strings.TrimSpace(errorText(list.err))
			msg, _ := agent.Explain(raw)
			if msg == "" {
				msg, _, _ = strings.Cut(raw, "\n")
			}
			if len(msg) > 80 {
				msg = msg[:80] + "…"
			}
			m.Item(msg).Disabled(true)
			return
		}
		modelItems(m, list.models, func(m *ui.Menu, label, id string) {
			if m.Item(label).Checked(chosen == id).Chosen() {
				setModel(id)
			}
		})
	}).Height(26).Tooltip("The model that reviews: " + cmp.Or(chosen, cmp.Or(list.def, a.Label+"'s default")))
}

// modelName is a model's name on its button: its label, without its
// provider's.
func modelName(id string, models []agent.Model) string {
	name := id
	if i := slices.IndexFunc(models, func(md agent.Model) bool { return md.ID == id }); i >= 0 {
		name = models[i].Label
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// modelItems adds models to a menu: in a submenu for each provider, and in
// one for each maker of the models of the providers that serve others',
// as vercel/anthropic/claude-sonnet-4.6.
func modelItems(m *ui.Menu, models []agent.Model, item func(m *ui.Menu, label, id string)) {
	vendor := func(md agent.Model) string {
		if v, _, ok := strings.Cut(md.Label, "/"); ok {
			return v
		}
		return ""
	}
	for _, p := range groupModels(models, func(md agent.Model) string { return md.Provider }) {
		if p.key == "" {
			for _, md := range p.models {
				item(m, md.Label, md.ID)
			}
			continue
		}
		m.Submenu(p.key, func(m *ui.Menu) {
			for _, v := range groupModels(p.models, vendor) {
				if v.key == "" {
					for _, md := range v.models {
						item(m, md.Label, md.ID)
					}
					continue
				}
				m.Submenu(v.key, func(m *ui.Menu) {
					for _, md := range v.models {
						item(m, strings.TrimPrefix(md.Label, v.key+"/"), md.ID)
					}
				})
			}
		})
	}
}

type modelGroup struct {
	key    string
	models []agent.Model
}

// groupModels groups models by a key, in the order of their first.
func groupModels(models []agent.Model, key func(agent.Model) string) []modelGroup {
	var groups []modelGroup
	at := map[string]int{}
	for _, md := range models {
		k := key(md)
		i, ok := at[k]
		if !ok {
			i = len(groups)
			at[k] = i
			groups = append(groups, modelGroup{key: k})
		}
		groups[i].models = append(groups[i].models, md)
	}
	return groups
}

// agentModels are the models of an agent, as it listed them.
type agentModels struct {
	models []agent.Model
	// def is the model the agent uses when none is chosen, "" when it does
	// not tell.
	def       string
	err       error
	done      bool
	fetching  bool
	fetchedAt time.Time
}

var modelsFound struct {
	sync.Mutex
	byAgent map[string]*agentModels
}

// modelsOf returns the models of an agent as last listed: they are
// listed off the main thread, when first asked for and again once a few
// minutes old, as providers come and go.
func modelsOf(a agent.Agent, dir string) agentModels {
	modelsFound.Lock()
	defer modelsFound.Unlock()
	if modelsFound.byAgent == nil {
		modelsFound.byAgent = map[string]*agentModels{}
	}
	list := modelsFound.byAgent[a.Name]
	if list == nil {
		list = &agentModels{}
		modelsFound.byAgent[a.Name] = list
	}
	if detectAgents && !list.fetching && (!list.done || time.Since(list.fetchedAt) > 5*time.Minute) {
		list.fetching = true
		go func() {
			models, def, err := agent.Models(context.Background(), a, dir)
			modelsFound.Lock()
			list.models, list.def, list.err = models, def, err
			list.done, list.fetching, list.fetchedAt = true, false, time.Now()
			modelsFound.Unlock()
			invalidateWindows()
		}()
	}
	return *list
}

// invalidateWindows builds every window again.
func invalidateWindows() {
	windowsMu.Lock()
	for _, w := range windows {
		w.invalidate()
	}
	windowsMu.Unlock()
}

// detectAgents is set by the app: tests look for no agents.
var detectAgents bool

var agentsFound struct {
	sync.Mutex
	list          []agent.Agent
	started, done bool
}

// installedAgents returns the agents installed, once known: they are
// looked for once, off the main thread.
func installedAgents() ([]agent.Agent, bool) {
	agentsFound.Lock()
	defer agentsFound.Unlock()
	if !agentsFound.started && detectAgents {
		agentsFound.started = true
		go func() {
			list := agent.Installed()
			agentsFound.Lock()
			agentsFound.list, agentsFound.done = list, true
			agentsFound.Unlock()
			invalidateWindows()
		}()
	}
	return agentsFound.list, agentsFound.done
}

// chooseGrouping groups the files as the user chose. Grouping by an AI
// review there is not yet offers one: an agent runs only when asked.
func (w *window) chooseGrouping(mode groupMode) {
	if mode == groupAI && w.analysisResult() == nil {
		w.groupPref = groupAI
		st := w.analysisFor(w.source)
		if !st.running {
			st.offered, st.err, st.errDetail = true, "", ""
		}
		w.rowsDirty = true
		w.list.ScrollTo(0, ui.Start)
		return
	}
	w.setGrouping(mode)
}

// reviewWithAI asks the agent for a review, or shows the one there is.
func (w *window) reviewWithAI() {
	st := w.analyses[w.source]
	if st != nil && (st.result != nil || st.running) {
		w.rowsDirty = true
		w.list.ScrollTo(0, ui.Start)
		return
	}
	w.runAnalysis()
}

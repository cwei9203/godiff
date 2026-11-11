package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/git"
)

// keyInput is an analysis of a change, keyed.
var keyInput = analysisKeyInput{
	SchemaVersion: 1,
	PromptVersion: "v1",
	Agent:         "claude",
	Model:         "sonnet",
	Locale:        "zh-CN",
	Root:          "/repo",
	Source:        "0:",
	Fingerprints:  map[string]string{"b.go": "2", "a.go": "1"},
}

func TestAnalysisKey(t *testing.T) {
	key := analysisKey(keyInput)
	if len(key) != 64 {
		t.Fatalf("key %q is not a sha256", key)
	}
	if key != analysisKey(keyInput) {
		t.Error("the same analysis keys two ways")
	}
	// The files enter the key sorted, whatever order the map gives.
	sorted := keyInput
	sorted.Fingerprints = map[string]string{"a.go": "1", "b.go": "2"}
	if analysisKey(sorted) != key {
		t.Error("the order of the files changes the key")
	}
	// Everything that makes the review another review changes it.
	for _, tc := range []struct {
		what  string
		delta func(in *analysisKeyInput)
	}{
		{"another model", func(in *analysisKeyInput) { in.Model = "opus" }},
		{"the default model", func(in *analysisKeyInput) { in.Model = "" }},
		{"another agent", func(in *analysisKeyInput) { in.Agent = "codex" }},
		{"another prompt", func(in *analysisKeyInput) { in.PromptVersion = "v2" }},
		{"another contract", func(in *analysisKeyInput) { in.SchemaVersion = 2 }},
		{"another language", func(in *analysisKeyInput) { in.Locale = "en-US" }},
		{"another change", func(in *analysisKeyInput) { in.Source = "1:abc" }},
		{"another file", func(in *analysisKeyInput) { in.Fingerprints = map[string]string{"a.go": "1", "b.go": "2", "c.go": "3"} }},
		{"another content", func(in *analysisKeyInput) { in.Fingerprints = map[string]string{"a.go": "1", "b.go": "changed"} }},
		{"another path", func(in *analysisKeyInput) { in.Fingerprints = map[string]string{"a.go": "1", "d.go": "2"} }},
	} {
		in := keyInput
		in.Fingerprints = map[string]string{"a.go": "1", "b.go": "2"}
		tc.delta(&in)
		if got := analysisKey(in); got == key {
			t.Errorf("%s keys as the analysis it is not", tc.what)
		}
	}
}

func TestAnalysisLocale(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"zh_CN.UTF-8", "zh-CN"},
		{"en_US.UTF-8", "en-US"},
		{"", ""},
	} {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", tc.lang)
		if got := analysisLocale(); got != tc.want {
			t.Errorf("LANG %q: locale %q, want %q", tc.lang, got, tc.want)
		}
	}
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	if got := analysisLocale(); got != "de-DE" {
		t.Errorf("LC_ALL: locale %q", got)
	}
}

func TestSavedAnalysisKeyed(t *testing.T) {
	repo, err := git.Open(testRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	old := state.path
	state.path = filepath.Join(t.TempDir(), "state.json")
	t.Cleanup(func() { state.path = old })
	src := source{}
	save := func(model string) {
		w := newWindow(repo, src)
		withSettings(t, func(s *Settings) { s.AIAgent, s.AIModel = "claude", model })
		w.settings = cfg.Get()
		a := &agent.Analysis{
			Agent: "claude", Model: model, Locale: analysisLocale(),
			OverallSummary: "One read path.",
			Groups:         []agent.Group{{Key: "cache", Label: "Cache", FilePaths: []string{"main.go"}}},
		}
		fingerprints := map[string]string{"main.go": "fingerprint"}
		a.Normalize()
		saveAnalysis(analysisPath(repo.Root, src), repo.Root, w.analysisKeyOf(src, a, fingerprints), a, fingerprints)
	}
	// loaded is the review a window reads of the source, as the settings
	// stand now.
	loaded := func() *agent.Analysis {
		w := newWindow(repo, src)
		if st := w.analyses[src]; st != nil {
			return st.result
		}
		return nil
	}

	save("sonnet")
	withSettings(t, func(s *Settings) { s.AIAgent, s.AIModel = "claude", "sonnet" })
	if a := loaded(); a == nil || a.OverallSummary != "One read path." {
		t.Fatalf("the review of these settings did not load: %+v", a)
	}
	// Another model is another review: what it wrote does not answer for it.
	withSettings(t, func(s *Settings) { s.AIAgent, s.AIModel = "claude", "opus" })
	if a := loaded(); a != nil {
		t.Errorf("the review of another model loaded: %+v", a)
	}
	// One saved before keys, or under another contract, is no review either.
	withSettings(t, func(s *Settings) { s.AIAgent, s.AIModel = "claude", "sonnet" })
	data, _ := os.ReadFile(analysisPath(repo.Root, src))
	var entry savedAnalysis
	json.Unmarshal(data, &entry)
	entry.Key = ""
	data, _ = json.Marshal(entry)
	os.WriteFile(analysisPath(repo.Root, src), data, 0o644)
	if a := loaded(); a != nil {
		t.Errorf("a review saved without a key loaded: %+v", a)
	}
	entry.Analysis.SchemaVersion = agent.SchemaVersion + 1
	entry.Key = analysisKey(analysisKeyInput{SchemaVersion: agent.SchemaVersion, PromptVersion: agent.PromptVersion, Agent: "claude", Model: "sonnet", Locale: entry.Analysis.Locale, Root: repo.Root, Fingerprints: entry.Fingerprints})
	data, _ = json.Marshal(entry)
	os.WriteFile(analysisPath(repo.Root, src), data, 0o644)
	if a := loaded(); a != nil {
		t.Errorf("a review of another contract loaded: %+v", a)
	}
}

// piNotSignedIn is what Pi prints when it is signed in to no model provider,
// verbatim, paths into its install and all.
const piNotSignedIn = `No models available. Use /login to log into a provider via OAuth or API key. See:
/Users/ada/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/providers.md
/Users/ada/.pi/agent/install/releases/1.5.1/node_modules/@earendil-works/pi-coding-agent/docs/models.md`

// TestAnalysisExplainsFailures is the report the translation layer answers:
// the card says what to do, and the agent's own words are found under it, not
// over it.
func TestAnalysisExplainsFailures(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	old := runAgent
	t.Cleanup(func() { runAgent = old })
	runAgent = func(ctx context.Context, a agent.Agent, req agent.Request, progress func(agent.Progress)) (*agent.Analysis, error) {
		return nil, errors.New("Pi failed: " + piNotSignedIn)
	}
	withSettings(t, func(s *Settings) { s.AIAgent = "pi" })
	w.settings = cfg.Get()
	w.runAnalysis()

	st := w.analyses[w.source]
	if st.running {
		t.Fatal("the review never ended")
	}
	if st.err != "Pi names no models. Sign in to a provider in a terminal, and check PI_CODING_AGENT_DIR points at your Pi agent directory, then try again." {
		t.Errorf("the error is %q, want the sentence to act on", st.err)
	}
	if strings.Contains(st.err, "node_modules") {
		t.Errorf("the error carries a path of the install: %q", st.err)
	}
	// The raw text is kept, whole.
	if st.errDetail != "Pi failed: "+piNotSignedIn {
		t.Errorf("the detail is %q", st.errDetail)
	}
	tt.Frame()
	if !tt.HasText("Pi names no models.") {
		t.Errorf("no sentence in %q", tt.Texts())
	}
	if tt.HasText("node_modules") {
		t.Errorf("the card shows the raw text unasked: %q", tt.Texts())
	}
	// It is a click away, for a report or a bug.
	if err := tt.Click("Show what it said"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("node_modules") {
		t.Errorf("the detail is not shown on request: %q", tt.Texts())
	}
}

// TestAnalysisShowsUnknownFailures is the other half: a failure godiff does
// not know is shown as it came, with nothing invented over it.
func TestAnalysisShowsUnknownFailures(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	old := runAgent
	t.Cleanup(func() { runAgent = old })
	runAgent = func(ctx context.Context, a agent.Agent, req agent.Request, progress func(agent.Progress)) (*agent.Analysis, error) {
		return nil, errors.New("Claude Code failed: it answered nothing")
	}
	withSettings(t, func(s *Settings) { s.AIAgent = "claude" })
	w.settings = cfg.Get()
	w.runAnalysis()
	st := w.analyses[w.source]
	if st.err != "Claude Code failed: it answered nothing" || st.errDetail != st.err {
		t.Errorf("the error is %q, detail %q", st.err, st.errDetail)
	}
	tt.Frame()
	if !tt.HasText("it answered nothing") || tt.HasText("Show what it said") {
		t.Errorf("the card of a failure it does not know: %q", tt.Texts())
	}
}

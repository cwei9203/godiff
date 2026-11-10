package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		saveAnalysis(analysisPath(repo.Root, src), w.analysisKeyOf(src, a, fingerprints), a, fingerprints)
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

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/github"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settingsOn renders the window of the settings at a page, as a window would
// draw it, without one: the tester answers for the window.
func settingsOn(t *testing.T, page string) (*settingsWindow, *ui.Tester) {
	t.Helper()
	sw := newSettingsWindow()
	sw.page = page
	sw.refresh(cfg.Get())
	sw.loadCacheSize()
	tt := ui.NewTester(sw.view, 720, 560)
	tt.Frame()
	tt.Frame()
	return sw, tt
}

// closeSettings forgets the window of the settings a test opened.
func closeSettings(t *testing.T) {
	t.Helper()
	settingsMu.Lock()
	settingsWin = nil
	settingsMu.Unlock()
}

// TestSettingsOpensOneWindow checks ⌘,: the settings open in a window of their
// own, and asking again comes back to that one rather than opening a second.
func TestSettingsOpensOneWindow(t *testing.T) {
	closeSettings(t)
	mounts, focused := 0, 0
	oldMount, oldFocus := mountSettingsWindow, focusSettingsWindow
	mountSettingsWindow = func(sw *settingsWindow) *mygo.Window { mounts++; return nil }
	focusSettingsWindow = func(sw *settingsWindow) { focused++ }
	t.Cleanup(func() {
		mountSettingsWindow, focusSettingsWindow = oldMount, oldFocus
		closeSettings(t)
	})

	openSettings()
	settingsMu.Lock()
	first := settingsWin
	settingsMu.Unlock()
	if mounts != 1 || first == nil {
		t.Fatalf("the settings opened %d windows", mounts)
	}
	if first.page != settingsAppearance {
		t.Errorf("the settings open at %q, want the appearance", first.page)
	}

	openSettings()
	openSettings()
	settingsMu.Lock()
	again := settingsWin
	settingsMu.Unlock()
	if mounts != 1 {
		t.Errorf("⌘, opened %d windows, want the one", mounts)
	}
	if again != first {
		t.Error("⌘, opened another window of the settings")
	}
	if focused != 2 {
		t.Errorf("⌘, came to the settings %d times, want the 2 after the first", focused)
	}

	// Closed, the next ⌘, opens it anew.
	closeSettings(t)
	openSettings()
	if mounts != 2 {
		t.Errorf("⌘, after the window closed made %d windows, want 2", mounts)
	}
	closeSettings(t)
}

// TestSettingsWriteReachesTheReview checks a switch of the settings: the file
// view, changed in the window, is the settings' at once, the review lays its
// files out the other way, and the file keeps it for the next launch.
func TestSettingsWriteReachesTheReview(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	if w.oneFile() {
		t.Fatal("the review does not start with every file stacked")
	}
	_, st := settingsOn(t, settingsAppearance)
	if !st.HasText("All files stacked") {
		t.Fatalf("the file view is not in the window: %q", st.Texts())
	}

	// One file at a time, chosen in the settings window.
	if err := st.Click("One file at a time"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Get().FileView; got != fileViewOne {
		t.Fatalf("the file view is %q after the click", got)
	}
	// The review hears of it, as it does from a menu, and shows one file.
	w.applySettings(cfg.Get())
	tt.Frame()
	if !w.oneFile() {
		t.Error("the review shows every file after the window chose one")
	}

	// Written, so that a launch after this one opens the same way.
	data, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"fileView": "one"`) {
		t.Errorf("the settings file does not hold the file view: %s", data)
	}
}

// TestSettingsAgentsAndModels checks the AI page: the agents it lists are the
// ones the machine has, the models are those of the agent chosen, and an agent
// that lists none takes a model typed by hand.
func TestSettingsAgentsAndModels(t *testing.T) {
	withAgents(t,
		[]agent.Agent{{Name: "claude", Label: "Claude Code"}, {Name: "codex", Label: "Codex"}},
		map[string]*agentModels{
			"claude": {done: true, models: []agent.Model{{ID: "sonnet", Label: "Sonnet"}, {ID: "opus", Label: "Opus"}}},
			"codex":  {done: true, err: errors.New("codex debug models: not logged in")},
		},
	)
	withSettings(t, func(s *Settings) { s.AIAgent, s.AIModel = "", "" })
	_, tt := settingsOn(t, settingsAI)
	if !tt.HasText("2 agents are installed") || !tt.HasText("Automatic") {
		t.Fatalf("the page does not say what was found: %q", tt.Texts())
	}
	// The agents listed are the ones the machine has, with their versions.
	if err := tt.Click("Automatic"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Claude Code", "Codex"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not among the agents: %q", want, tt.Texts())
		}
	}

	// Codex, which lists no model: the id is typed.
	if err := tt.Click("Codex"); err != nil {
		t.Fatalf("choosing an agent: %v", err)
	}
	tt.Frame()
	if got := cfg.Get().AIAgent; got != "codex" {
		t.Fatalf("the agent is %q after the click", got)
	}
	if !tt.HasText("The agent could not list its models: type one. codex debug models: not logged in") {
		t.Fatalf("the page does not ask for a model by hand: %q", tt.Texts())
	}
	// The field takes an id, as the config file would: it is the control
	// beside the label of the model.
	label, ok := tt.Find("The agent's default")
	if !ok {
		label, ok = tt.Find("Model")
	}
	if !ok {
		t.Fatalf("no field of the model: %q", tt.Texts())
	}
	tt.ClickAt(label.X+label.W+80, label.Y+label.H/2)
	tt.Type("gpt-5-codex")
	tt.Frame()
	if got := cfg.Get().AIModel; got != "gpt-5-codex" {
		t.Errorf("the model typed by hand is %q", got)
	}
}

// TestSettingsLanguageSwitches checks the window says the language the
// interface says: its title, and the groups of the settings, in Chinese and
// back in English.
func TestSettingsLanguageSwitches(t *testing.T) {
	useEnglish(t)
	sw, tt := settingsOn(t, settingsAppearance)
	if got := sw.title(); got != "Settings" {
		t.Errorf("the window is titled %q", got)
	}
	if !tt.HasText("Appearance") || !tt.HasText("Workspace") || !tt.HasText("Advanced") {
		t.Fatalf("the groups are not in English: %q", tt.Texts())
	}

	sw.set(func(s *Settings) { s.UILocale = "zh-Hans" })
	applyUiLocale(cfg.Get())
	tt.Frame()
	tt.Frame()
	if got := sw.title(); got != "设置" {
		t.Errorf("the window is titled %q in Chinese", got)
	}
	for _, want := range []string{"外观", "审查", "工作区", "高级", "界面语言", "代码字体", "字号"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the Chinese settings: %q", want, tt.Texts())
		}
	}
	for _, gone := range []string{"Appearance", "Workspace", "Interface language"} {
		if tt.HasText(gone) {
			t.Errorf("%q stayed in the Chinese settings: %q", gone, tt.Texts())
		}
	}

	sw.set(func(s *Settings) { s.UILocale = "en" })
	applyUiLocale(cfg.Get())
	tt.Frame()
	if got := sw.title(); got != "Settings" {
		t.Errorf("the window is titled %q after English came back", got)
	}
	if !tt.HasText("Appearance") || tt.HasText("外观") {
		t.Errorf("English did not come back: %q", tt.Texts())
	}
}

// TestSettingsClearCacheAsks checks the Advanced page: the cache is deleted
// once the alert is answered, not when the button is pressed.
func TestSettingsClearCacheAsks(t *testing.T) {
	withStateFile(t)
	dir := analysesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.json", "b.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runs := runsDir()
	if err := os.MkdirAll(runs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runs, "fix-1.log"), []byte("a run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sw, tt := settingsOn(t, settingsAdvanced)
	if !tt.HasText("The cache takes 10 B.") {
		t.Errorf("the page does not say what the cache takes: %q", tt.Texts())
	}
	if err := tt.Click("Clear cache"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !sw.confirmClear || !tt.HasText("Clear the cache?") {
		t.Fatalf("the button does not ask first: %q", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(dir, "a.json")); err != nil {
		t.Error("the cache went before the alert was answered")
	}

	// Cancelled, it stays.
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := os.Stat(filepath.Join(dir, "a.json")); err != nil {
		t.Error("Cancel deleted the cache")
	}

	// Answered, it goes.
	if err := tt.Click("Clear cache"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the reviews are still there: %v", err)
	}
	if _, err := os.Stat(runs); !os.IsNotExist(err) {
		t.Errorf("the logs are still there: %v", err)
	}
	if sw.cacheBytes != 0 {
		t.Errorf("the cache still takes %d bytes", sw.cacheBytes)
	}
	if !tt.HasText("Cache cleared: 10 B freed.") {
		t.Errorf("the page does not say what clearing freed: %q", tt.Texts())
	}
}

// TestSettingsGitHubAccount checks the GitHub page: the token is asked of
// GitHub, whose account it is shows with its picture, and one GitHub refuses
// says so.
func TestSettingsGitHubAccount(t *testing.T) {
	old := githubViewer
	githubViewer = func(_ context.Context, token string) (github.User, error) {
		if token != "good" {
			return github.User{}, errors.New("Bad credentials")
		}
		return github.User{Login: "egoist", AvatarURL: "https://example.com/a.png"}, nil
	}
	t.Cleanup(func() { githubViewer = old })
	withSettings(t, func(s *Settings) { s.GithubToken = "good" })

	sw, tt := settingsOn(t, settingsGitHub)
	sw.loadGitHub()
	tt.Frame()
	if !tt.HasText("Signed in as egoist") || !tt.HasText("egoist") {
		t.Errorf("the page does not say whose the token is: %q", tt.Texts())
	}

	swith := sw
	swith.token = "bad"
	swith.set(func(s *Settings) { s.GithubToken = "bad" })
	swith.loadGitHub()
	tt.Frame()
	if !tt.HasText("GitHub refused the token: Bad credentials") {
		t.Errorf("the page does not say the token was refused: %q", tt.Texts())
	}
}

// withAgents answers for the machine: the agents installed, and the models
// each of them lists.
func withAgents(t *testing.T, installed []agent.Agent, models map[string]*agentModels) {
	t.Helper()
	agentsFound.Lock()
	oldList, oldStarted, oldDone := agentsFound.list, agentsFound.started, agentsFound.done
	agentsFound.list, agentsFound.started, agentsFound.done = installed, true, true
	agentsFound.Unlock()

	modelsFound.Lock()
	oldModels := modelsFound.byAgent
	modelsFound.byAgent = models
	modelsFound.Unlock()

	t.Cleanup(func() {
		agentsFound.Lock()
		agentsFound.list, agentsFound.started, agentsFound.done = oldList, oldStarted, oldDone
		agentsFound.Unlock()
		modelsFound.Lock()
		modelsFound.byAgent = oldModels
		modelsFound.Unlock()
	})
}

// TestSettingsSnapshot saves the frames of the settings window, the three
// pages the design is looked at, in each language.
func TestSettingsSnapshot(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	withAgents(t,
		[]agent.Agent{{Name: "claude", Label: "Claude Code"}, {Name: "pi", Label: "Pi"}},
		map[string]*agentModels{
			"claude": {done: true, models: []agent.Model{{ID: "opus", Label: "Opus"}, {ID: "sonnet", Label: "Sonnet"}}},
			"pi":     {done: true},
		},
	)
	withSettings(t, func(s *Settings) { s.AIAgent = "claude" })
	for _, c := range []struct{ prefix, tag string }{{"en", "en"}, {"zh", "zh-Hans"}} {
		for _, page := range []string{settingsAppearance, settingsAI, settingsWorkspace, settingsAdvanced} {
			sw, tt := settingsOn(t, page)
			sw.set(func(s *Settings) { s.UILocale = c.tag })
			applyUiLocale(cfg.Get())
			tt.Frame()
			tt.Frame()
			snapshot(t, tt, c.prefix+"-settings-"+page)

			dark := ui.NewTester(sw.view, 720, 560)
			dark.SetDark(true)
			dark.Frame()
			dark.Frame()
			snapshot(t, dark, c.prefix+"-settings-"+page+"-dark")
		}
	}
}

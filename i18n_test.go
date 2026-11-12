package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// testFollowedLanguage is the language MyGo would show for the reader whose
// system languages the settings follow: a test has no main loop for MyGo to
// work it out on, so it answers instead.
func testFollowedLanguage() string { return "en" }

// useEnglish puts the interface back in English, whatever a test said, and the
// settings with it.
func useEnglish(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		followedLanguage = testFollowedLanguage
		interfaceLanguage = "en"
		cfg.Update(func(s *Settings) { s.UILocale = "" })
	})
}

// TestInterfaceLanguageSwitches checks the switch of the interface's language:
// the settings' uiLocale says another language, the words of the surface follow
// it at once, and the other language comes back.
func TestInterfaceLanguageSwitches(t *testing.T) {
	useEnglish(t)
	w, tt := kindWindow(t)
	for _, want := range []string{"View", "Filter files"} {
		if !tt.HasText(want) {
			t.Fatalf("%q is not in the English surface: %q", want, tt.Texts())
		}
	}

	w.applySetting(func(s *Settings) { s.UILocale = "zh-Hans" })
	tt.Frame()
	tt.Frame()
	// The toolbar's menu, the sidebar's field and its folds, and the groups'
	// headings, each in the language asked for.
	for _, want := range []string{"视图", "筛选文件", "收起全部", "展开全部", "个文件"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the Chinese surface: %q", want, tt.Texts())
		}
	}
	if !slices.ContainsFunc(tt.Texts(), func(s string) bool { return strings.HasPrefix(s, "已看 ") }) {
		t.Errorf("no group says how much of it is viewed in Chinese: %q", tt.Texts())
	}
	for _, gone := range []string{"View", "Filter files", "Fold all"} {
		if tt.HasText(gone) {
			t.Errorf("%q stayed in the Chinese surface: %q", gone, tt.Texts())
		}
	}

	w.applySetting(func(s *Settings) { s.UILocale = "en" })
	tt.Frame()
	if !tt.HasText("View") || !tt.HasText("Filter files") {
		t.Errorf("English did not come back: %q", tt.Texts())
	}
	if tt.HasText("视图") || tt.HasText("筛选文件") {
		t.Errorf("the Chinese words stayed: %q", tt.Texts())
	}
}

// TestInterfaceLanguageFollowsTheSystem checks uiLocale empty: the interface
// says the language MyGo shows for the reader's languages, and the settings'
// choice is what overrides it.
func TestInterfaceLanguageFollowsTheSystem(t *testing.T) {
	useEnglish(t)
	followedLanguage = func() string { return "zh-Hans" }
	w, tt := kindWindow(t)
	w.applySetting(func(s *Settings) { s.UILocale = "en" })
	tt.Frame()
	if !tt.HasText("View") {
		t.Fatalf("the chosen language is not English: %q", tt.Texts())
	}

	// No language chosen: the reader's, which is MyGo's to work out.
	w.applySetting(func(s *Settings) { s.UILocale = "" })
	tt.Frame()
	if !tt.HasText("视图") {
		t.Errorf("the interface did not follow the reader's language: %q", tt.Texts())
	}
}

// TestI18nSnapshots saves the frames of the language pass, at a wide window and
// a narrow one, in each language, to compare the two: the words of one must not
// break the layout of the other.
func TestI18nSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	useEnglish(t)
	for _, c := range []struct{ prefix, tag string }{{"en", "en"}, {"zh", "zh-Hans"}} {
		w, tt := designWindow(t)
		w.applySetting(func(s *Settings) { s.UILocale = c.tag })
		tt.Frame()
		tt.Frame()
		snapshot(t, tt, c.prefix+"-wide")

		narrow := ui.NewTester(w.view, 820, 760)
		narrow.Frame()
		narrow.Frame()
		snapshot(t, narrow, c.prefix+"-narrow")
	}
}

// TestCatalogsAgree checks that every message of the source catalog has its
// translation, and no translation has a message the source lacks: what
// `mygo i18n check` reports, without the CLI.
func TestCatalogsAgree(t *testing.T) {
	dir := "locales"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys := func(file string) []string {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]any
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var out []string
		for key := range messages {
			if !strings.HasPrefix(key, "@") {
				out = append(out, key)
			}
		}
		slices.Sort(out)
		return out
	}
	source := keys("en.json")
	if len(source) == 0 {
		t.Fatal("the source catalog has no message")
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || name == "en.json" {
			continue
		}
		got := keys(name)
		for _, key := range source {
			if !slices.Contains(got, key) {
				t.Errorf("%s: no %q", name, key)
			}
		}
		for _, key := range got {
			if !slices.Contains(source, key) {
				t.Errorf("%s: %q is not in the source catalog", name, key)
			}
		}
	}
}

// TestAICardSpeaksChinese checks the agent's card in its three states, in the
// interface's language: offered, at work, and the review it gave.
func TestAICardSpeaksChinese(t *testing.T) {
	useEnglish(t)
	w, tt := designWindow(t)
	w.applySetting(func(s *Settings) { s.UILocale = "zh-Hans" })
	tt.Frame()
	tt.Frame()

	st := w.analysisFor(w.source)
	result := st.result
	for _, want := range []string{"AI 审查", "再次审查", "需要留意", "按本次审查给文件分组", "个分组", "条注释"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the result card: %q", want, tt.Texts())
		}
	}

	// At work: the steps, the stop, and what it reads now.
	st.result = nil
	st.running = true
	st.agent = "Claude Code"
	st.started = w.now
	st.progress = agent.Progress{Kind: agent.ProgressReading, Paths: []string{"app.go", "app_test.go"}}
	tt.Frame()
	for _, want := range []string{"AI 审查", "停止", "正在思考改动", "正在阅读 2 个文件"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the running card: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Review Again") || tt.HasText("Review with AI") {
		t.Errorf("the English words stayed in the running card: %q", tt.Texts())
	}

	// Offered, and not started.
	st.running, st.result, st.offered = false, nil, true
	tt.Frame()
	for _, want := range []string{"AI 审查", "用 AI 审查", "自动"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the offered card: %q", want, tt.Texts())
		}
	}

	st.result = result
}

// TestCommandBarSpeaksChinese checks the command bar's titles and hints in the
// interface's language.
func TestCommandBarSpeaksChinese(t *testing.T) {
	useEnglish(t)
	w, tt := designWindow(t)
	w.applySetting(func(s *Settings) { s.UILocale = "zh-Hans" })
	w.paletteOpen = true
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"打开设置", "下一个未看的文件", "收起全部", "在差异中搜索"} {
		if !tt.HasText(want) {
			t.Errorf("%q is not in the command bar: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Open settings") || tt.HasText("Fold all") {
		t.Errorf("the English words stayed in the command bar: %q", tt.Texts())
	}
}

// TestI18nSnapshotsWelcomeAndCard saves the two surfaces this batch reworked:
// the welcome page with recent repositories and without, and the agent's card
// in its three states, in each language.
func TestI18nSnapshotsWelcomeAndCard(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	for _, c := range []struct{ tag, prefix string }{{"en", "en"}, {"zh-Hans", "zh"}} {
		speak(t, c.tag)
		_, tt := welcomeWith(t, nil, "/src/godiff", "/src/mygo", "/src/fate")
		snapshot(t, tt, "welcome-"+c.prefix)
		_, empty := welcomeWith(t, nil)
		snapshot(t, empty, "welcome-empty-"+c.prefix)
	}

	useEnglish(t)
	w, tt := designWindow(t)
	st := w.analysisFor(w.source)
	result := st.result
	for _, c := range []struct{ tag, prefix string }{{"en", "en"}, {"zh-Hans", "zh"}} {
		w.applySetting(func(s *Settings) { s.UILocale = c.tag })
		st.running, st.result, st.offered = false, result, false
		tt.Frame()
		snapshot(t, tt, "ai-result-"+c.prefix)

		st.result, st.offered, st.running = nil, true, false
		tt.Frame()
		snapshot(t, tt, "ai-offer-"+c.prefix)

		st.offered, st.running = false, true
		st.agent = "Claude Code"
		st.started = w.now
		st.progress = agent.Progress{Kind: agent.ProgressReading, Paths: []string{"app.go", "app_test.go", "go.mod"}}
		tt.Frame()
		snapshot(t, tt, "ai-running-"+c.prefix)
	}
}

// TestGenLocalesScript checks the script that writes the catalog: it runs from
// a repository whose path has a space in it, which is what it is for, and the
// file it writes is the one the catalogs make.
func TestGenLocalesScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is a shell script")
	}
	src, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// The repository copied where the generator can work, under a folder whose
	// name has a space in it, as ~/Library/Application Support has.
	dir := filepath.Join(t.TempDir(), "a folder with a space")
	if out, err := exec.Command("cp", "-R", src+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copy the repository to %s: %v\n%s", dir, err, out)
	}
	want, err := os.ReadFile(filepath.Join(src, "locales", "catalog_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/sh", filepath.Join(dir, "scripts", "gen-locales.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("the script failed in %s: %v\n%s", dir, err, out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "locales", "catalog_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the script wrote another catalog than the one the repository has:\n%s", out)
	}
}

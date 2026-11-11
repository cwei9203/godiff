package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

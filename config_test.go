package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRecent(t *testing.T) {
	s := &store{}
	for i := range maxRecent + 2 {
		s.addRecent(fmt.Sprintf("/repo%d", i))
	}
	got := s.recent()
	if len(got) != maxRecent || got[0] != fmt.Sprintf("/repo%d", maxRecent+1) {
		t.Fatalf("got %v", got)
	}
	// Opening one again brings it first, once.
	if !s.addRecent("/repo5") {
		t.Error("moving a repository first changed nothing")
	}
	if s.addRecent("/repo5") {
		t.Error("the first repository changed the list")
	}
	got = s.recent()
	if got[0] != "/repo5" || len(got) != maxRecent || slices.Index(got[1:], "/repo5") >= 0 {
		t.Errorf("got %v", got)
	}
	s.clearRecent()
	if len(s.recent()) != 0 {
		t.Error("the list was not cleared")
	}
}

// TestSettingsDefaults checks the settings of a fresh install, the review's
// switches among them.
func TestSettingsDefaults(t *testing.T) {
	s := defaultSettings()
	for _, sw := range []struct {
		name string
		on   bool
	}{
		{"GroupingEnabled", s.GroupingEnabled},
		{"OverallSummary", s.OverallSummary},
		{"Notes", s.Notes},
	} {
		if !sw.on {
			t.Errorf("%s: off by default", sw.name)
		}
	}
	if s.SummaryExpanded {
		t.Error("SummaryExpanded: on by default, the summary would hide its first line")
	}
	if s.SummaryLocale != "" {
		t.Errorf("SummaryLocale: %q, not the language of the interface", s.SummaryLocale)
	}
	if s.MaxFiles != maxFilesDefault {
		t.Errorf("MaxFiles: %d, want %d", s.MaxFiles, maxFilesDefault)
	}
	// Normalizing what the defaults hold changes nothing.
	s.normalize()
	if !s.GroupingEnabled || !s.OverallSummary || s.SummaryExpanded || !s.Notes || s.MaxFiles != maxFilesDefault {
		t.Errorf("normalizing the defaults changed them: %+v", s)
	}
}

// TestSettingsNormalize checks that what cannot be is turned into a value
// that can.
func TestSettingsNormalize(t *testing.T) {
	for _, c := range []struct {
		name  string
		in    Settings
		check func(s Settings) error
	}{
		{
			name: "no files at all",
			in:   Settings{MaxFiles: 0},
			check: func(s Settings) error {
				if s.MaxFiles != maxFilesDefault {
					return fmt.Errorf("MaxFiles %d, want the default %d", s.MaxFiles, maxFilesDefault)
				}
				return nil
			},
		},
		{
			name: "a negative count of files",
			in:   Settings{MaxFiles: -12},
			check: func(s Settings) error {
				if s.MaxFiles != maxFilesDefault {
					return fmt.Errorf("MaxFiles %d, want the default %d", s.MaxFiles, maxFilesDefault)
				}
				return nil
			},
		},
		{
			name: "more files than may be asked for",
			in:   Settings{MaxFiles: maxFilesLimit * 3},
			check: func(s Settings) error {
				if s.MaxFiles != maxFilesLimit {
					return fmt.Errorf("MaxFiles %d, want %d", s.MaxFiles, maxFilesLimit)
				}
				return nil
			},
		},
		{
			name: "a few files, raised as asked",
			in:   Settings{MaxFiles: 50},
			check: func(s Settings) error {
				if s.MaxFiles != 50 {
					return fmt.Errorf("MaxFiles %d, want 50", s.MaxFiles)
				}
				return nil
			},
		},
		{
			name: "a language that is no tag",
			in:   Settings{SummaryLocale: "  zh CN! "},
			check: func(s Settings) error {
				if s.SummaryLocale != "" {
					return fmt.Errorf("SummaryLocale %q, want the interface's", s.SummaryLocale)
				}
				return nil
			},
		},
		{
			name: "a language that is one",
			in:   Settings{SummaryLocale: " zh-CN "},
			check: func(s Settings) error {
				if s.SummaryLocale != "zh-CN" {
					return fmt.Errorf("SummaryLocale %q, want zh-CN", s.SummaryLocale)
				}
				return nil
			},
		},
		{
			name: "switches turned off stay off",
			in:   Settings{GroupingEnabled: false, OverallSummary: false, Notes: false, SummaryExpanded: true},
			check: func(s Settings) error {
				if s.GroupingEnabled || s.OverallSummary || s.Notes || !s.SummaryExpanded {
					return fmt.Errorf("the switches changed: %+v", s)
				}
				return nil
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.in.normalize()
			if err := c.check(c.in); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestSettingsRoundTrip checks that the review's settings are written and
// read again as they were.
func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "godiff.jsonc")
	want := defaultSettings()
	want.GroupingEnabled = false
	want.OverallSummary = false
	want.SummaryExpanded = true
	want.Notes = false
	want.SummaryLocale = "zh-CN"
	want.MaxFiles = 500
	writer := &config{path: path, settings: want}
	writer.write(want)

	// A fresh config reads the file as the app starts.
	got := &config{path: path, settings: defaultSettings()}
	if !got.load() {
		t.Fatal("the file written was not read")
	}
	if s := got.Get(); s != want {
		t.Errorf("read %+v, wrote %+v", s, want)
	}
}

// TestSettingsLoadKeepsDefaults checks that the keys a file leaves out keep
// the settings of a fresh install, the review's switches among them.
func TestSettingsLoadKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "godiff.jsonc")
	// A file as an older version wrote it: no review switch at all, and
	// values that cannot be.
	data := `{
  // the review's switches are missing
  "settings": {
    "diffStyle": "unified",
    "maxFiles": 0,
    "summaryLocale": "not a tag",
  },
}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &config{path: path, settings: defaultSettings()}
	if !c.load() {
		t.Fatal("the file was not read")
	}
	s := c.Get()
	if s.DiffStyle != "unified" {
		t.Errorf("DiffStyle %q, want unified", s.DiffStyle)
	}
	if !s.GroupingEnabled || !s.OverallSummary || !s.Notes || s.SummaryExpanded {
		t.Errorf("the review's switches were not the defaults: %+v", s)
	}
	if s.MaxFiles != maxFilesDefault {
		t.Errorf("MaxFiles %d, want the default %d", s.MaxFiles, maxFilesDefault)
	}
	if s.SummaryLocale != "" {
		t.Errorf("SummaryLocale %q, want the interface's", s.SummaryLocale)
	}

	// A switch written off is read off.
	off := `{"settings": {"groupingEnabled": false, "overallSummary": false, "notes": false, "summaryExpanded": true}}`
	if err := os.WriteFile(path, []byte(off), 0o644); err != nil {
		t.Fatal(err)
	}
	c = &config{path: path, settings: defaultSettings()}
	if !c.load() {
		t.Fatal("the file was not read")
	}
	s = c.Get()
	if s.GroupingEnabled || s.OverallSummary || s.Notes || !s.SummaryExpanded {
		t.Errorf("the switches written were not read: %+v", s)
	}
}

// jsonFor writes the settings as the config file does.
func jsonFor(s Settings) (string, error) {
	data, err := json.MarshalIndent(map[string]any{"settings": s}, "", "  ")
	return string(data), err
}

// TestSettingsFileNames checks the keys the review's settings are kept
// under, which older and newer versions read alike.
func TestSettingsFileNames(t *testing.T) {
	data, err := jsonFor(defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"groupingEnabled", "overallSummary", "summaryExpanded", "notes", "summaryLocale", "maxFiles"} {
		if !strings.Contains(data, `"`+key+`"`) {
			t.Errorf("no %q in %s", key, data)
		}
	}
}

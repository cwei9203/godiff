package main

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// designWindow opens the review the design batches are looked at in: the six
// groups of kindRepo, with the agent's review atop them, one group needing
// care, and a note on a file and on a line of it.
func designWindow(t *testing.T) (*window, *ui.Tester) {
	t.Helper()
	w, tt := newTestWindow(t, kindRepo(t))
	w.groupPref = groupKind
	if ag, ok := agent.ByName("pi"); ok {
		st := w.analysisFor(w.source)
		st.result = &agent.Analysis{
			OverallSummary: "The change adds a log line, a test, a dependency and a workflow, and updates the docs.\nNothing here rewrites the app's surface.",
			Agent:          ag.Name,
			Model:          "sonnet",
			GeneratedAt:    time.Now().Add(-9 * time.Minute),
			Groups: []agent.Group{{
				Key:       "code",
				Label:     "Code",
				Summary:   "The run function logs once more.",
				Category:  agent.CategoryCore,
				Critical:  true,
				FilePaths: []string{"app.go"},
				FileNotes: []agent.FileNote{{Path: "app.go", Text: "The log line writes to stdout, which the CLI wraps.", Critical: true}},
				LineNotes: []agent.LineNote{{Path: "app.go", Side: "additions", Line: 3, Text: "This runs on every call."}},
			}},
		}
	}
	w.regroup()
	w.list.ScrollTo(0, ui.Start)
	tt.Frame()
	tt.Frame()
	return w, tt
}

// TestDesignSnapshots saves the frames of the design check, to compare one
// batch against the last: a wide window with every file stacked, one file at
// a time, a narrow window where the groups show as chips, and the dark theme.
func TestDesignSnapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	// The prefix names the batch: before-stack, after-stack, and so on.
	prefix := os.Getenv("GODIFF_SNAPSHOT_PREFIX")
	w, tt := designWindow(t)
	snapshot(t, tt, prefix+"stack")

	keepFileView(t)
	w.setFileView(fileViewOne)
	tt.Frame()
	tt.Frame()
	snapshot(t, tt, prefix+"one")

	w.setFileView(fileViewStack)
	w.sidebarShown = false
	narrow := ui.NewTester(w.view, 820, 760)
	narrow.Frame()
	narrow.Frame()
	snapshot(t, narrow, prefix+"narrow")

	w.sidebarShown = true
	dark := ui.NewTester(w.view, 1280, 860)
	dark.SetDark(true)
	dark.Frame()
	dark.Frame()
	snapshot(t, dark, prefix+"dark")
}

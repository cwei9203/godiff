package main

import (
	"os"
	"testing"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/mygo/ui"
)

// TestD4Snapshots saves the frames of the icon pass, to compare the sizes
// before it against the ones after: the tree beside the surface, the band of a
// group over a file's header, the toolbar, the agent's card, the bar of an undo,
// the settings window and the welcome page, in both themes and both languages.
//
// The prefix names the pass: "before-" or "after-".
func TestD4Snapshots(t *testing.T) {
	if os.Getenv("GODIFF_SNAPSHOTS") == "" {
		t.Skip("no GODIFF_SNAPSHOTS")
	}
	prefix := os.Getenv("GODIFF_SNAPSHOT_PREFIX")
	withAgents(t,
		[]agent.Agent{{Name: "claude", Label: "Claude Code"}, {Name: "pi", Label: "Pi"}},
		map[string]*agentModels{
			"claude": {done: true, models: []agent.Model{{ID: "opus", Label: "Opus"}}},
			"pi":     {done: true},
		},
	)
	withSettings(t, func(s *Settings) { s.AIAgent = "claude" })
	useEnglish(t)
	keepFileView(t)

	for _, theme := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		for _, lang := range []struct{ tag, name string }{{"en", "en"}, {"zh-Hans", "zh"}} {
			// The surface as it opens: the tree, the band of a group, a file's
			// header, the toolbar and the card of the agent.
			w, tt := designWindow(t)
			w.applySetting(func(s *Settings) { s.UILocale = lang.tag })
			name := prefix + theme.name + "-" + lang.name
			light, dark := tt, ui.NewTester(w.view, 1280, 860)
			dark.SetDark(true)
			at := func() *ui.Tester {
				if theme.dark {
					return dark
				}
				return light
			}
			frames(at())
			snapshot(t, at(), name+"-stack")

			// One file at a time: the row stepping between the files, and the
			// breadcrumb of the group its header carries.
			w.setFileView(fileViewOne)
			frames(at())
			snapshot(t, at(), name+"-one")

			// A file marked viewed: the tick of its header's button, and the
			// mark of its row in the tree.
			w.setViewed(w.files[0], true)
			frames(at())
			snapshot(t, at(), name+"-viewed")

			// The view of a commit, with the chip of the branch and its own
			// buttons.
			w.setFileView(fileViewStack)
			w.toggleCommit()
			frames(at())
			snapshot(t, at(), name+"-commit")
			w.toggleCommit()

			// The bar of an undo, after a fold of the whole tree.
			w.foldAll(true)
			frames(at())
			snapshot(t, at(), name+"-undo")
			w.undo = nil
			w.foldAll(false)

			// The settings window, on the page the icons of its list show and
			// on the one its form does.
			for _, page := range []string{settingsAppearance, settingsAI} {
				closeSettings(t)
				sw, tt := settingsOn(t, page)
				sw.set(func(s *Settings) { s.UILocale = lang.tag })
				applyUiLocale(cfg.Get())
				tt.SetDark(theme.dark)
				frames(tt)
				snapshot(t, tt, name+"-settings-"+page)
				closeSettings(t)
			}

			// The welcome page, whose one icon is the app's own.
			speak(t, lang.tag)
			_, welcome := welcomeWith(t, nil, "/src/godiff", "/src/mygo", "/src/fate")
			welcome.SetDark(theme.dark)
			frames(welcome)
			snapshot(t, welcome, name+"-welcome")
		}
	}
}

// frames draws the frame twice: the first pass lays the surface out, and the
// second one draws what the first one measured.
func frames(tt *ui.Tester) {
	tt.Frame()
	tt.Frame()
}

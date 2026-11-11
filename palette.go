package main

import (
	"strings"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/github"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// command is an action of the command bar.
type command struct {
	title string
	hint  string // a description, or the shortcut
	keys  string
	run   func()
	// soon is a command the review does not have yet: it shows, and does
	// nothing, rather than pretending it is not there.
	soon bool
}

// commands lists the command bar's actions, in its order.
func (w *window) commands() []command {
	selected := ""
	if w.current >= 0 && w.current < len(w.files) {
		selected = w.files[w.current].Path
	}
	layout := "Switch to unified"
	if !w.split() {
		layout = "Switch to split"
	}
	wrap := "Turn wrapping on"
	if w.settings.WordWrap {
		wrap = "Turn wrapping off"
	}
	// Going to the notes of the agent's review, when there are.
	notes := func(dir int) func() {
		if len(w.notes) == 0 {
			return nil
		}
		return func() { w.nextNote(dir) }
	}
	whitespace := "Show whitespace changes"
	if w.settings.ShowWhitespace {
		whitespace = "Hide whitespace changes"
	}
	// Writing the task file asks for the comments there are: with none, the
	// command has nothing to do and is left out of the bar.
	var exportTask func()
	if w.repo != nil && w.pendingComments() > 0 {
		exportTask = w.exportReviewTask
	}
	// Handing them to an agent asks for the comments there are and an agent
	// installed that can write: without either, it is left out as well.
	var fixComments func()
	if w.repo != nil && w.pendingComments() > 0 && !w.fixAtWork() {
		if _, ok := w.fixAgent(); ok {
			fixComments = w.openFixDialog
		}
	}
	// The keys of the files not viewed yet and of the notes trade places
	// with the file view: one file at a time, n steps the files and the
	// notes take ⌥n; every file stacked and n stays the notes'.
	unviewedKeys, noteKeys, noteBack := "", "N", "⇧N"
	if w.oneFile() {
		unviewedKeys, noteKeys, noteBack = "N", "⌥N", "⌥⇧N"
	}
	return []command{
		{title: "Focus file filter", keys: "⌘P", run: w.focusFilter},
		{title: "Find in diffs", keys: "⌘F", run: func() { w.finding = true }},
		{title: "Open commit", hint: "Review a commit", run: func() { w.openDialog(dialogCommit) }},
		{title: "Open branch", hint: "Compare with a branch", run: func() { w.openDialog(dialogBranch) }},
		{title: "Open pull request", hint: "Review a pull request on GitHub", run: func() { w.openDialog(dialogPull) }},
		{title: "Open folder", keys: "⌘O", run: openFolder},
		{title: "Show file tree", keys: "⌘1", run: func() { w.tab, w.sidebarShown = 0, true }},
		{title: "Show history", keys: "⌘2", run: func() { w.tab, w.sidebarShown = 1, true }},
		{title: "Show pull requests", keys: "⌘3", run: w.showPulls},
		{title: "Next unviewed file", hint: "The first file not viewed yet, round from this one", keys: unviewedKeys, run: func() { w.stepUnviewed(1) }},
		{title: "Review with AI", hint: w.agentHint(), keys: "⌘⇧I", run: w.reviewWithAI},
		{title: "Stop the AI review", run: w.stopAnalysis},
		{title: "Next AI note", keys: noteKeys, run: notes(1)},
		{title: "Previous AI note", keys: noteBack, run: notes(-1)},
		{title: "Group files by kind", run: func() { w.chooseGrouping(groupKind) }},
		{title: "Group files by AI review", run: func() { w.chooseGrouping(groupAI) }},
		{title: "Ungroup files", run: func() { w.chooseGrouping(groupNone) }},
		{title: "Show uncommitted changes", run: func() { w.setSource(w.launchWorkTree()) }},
		{title: "Commit…", run: func() {
			if w.source.kind == sourceWorkingTree && !w.commitOpen {
				w.toggleCommit()
			}
		}},
		{title: "Copy review comments", run: w.copyComments},
		{title: "Export review task", hint: ".review/REVIEW.md, for an agent", run: exportTask},
		{title: "Fix comments with an agent", hint: w.fixHint(), run: fixComments},
		{title: "Submit review", hint: "Approve, comment or request changes", run: w.openSubmit},
		{title: "Open pull request on GitHub", run: w.openPullOnGitHub},
		{title: "Copy review comments and close", run: func() {
			w.copyComments()
			if w.win != nil {
				w.win.Close()
			}
		}},
		{title: "Toggle viewed", hint: selected, run: func() {
			if w.current >= 0 && w.current < len(w.files) {
				f := w.files[w.current]
				w.setViewed(f, !w.isViewed(f))
			}
		}},
		{title: "Mark all viewed", hint: "Every file of the review, with an undo", run: w.markAllViewed},
		{title: "Reset review state", hint: "Nothing viewed, everything open", run: w.resetReviewState},
		{title: "Next file", hint: "One file at a time", keys: "]", run: func() { w.stepFile(1) }},
		{title: "Previous file", hint: "One file at a time", keys: "[", run: func() { w.stepFile(-1) }},
		{title: "Open file in editor", hint: selected, keys: "⌘⇧O", run: w.openCurrent},
		{title: "Toggle sidebar", keys: "⌘⇧B", run: w.toggleSidebar},
		{title: "Fold all", hint: "Every group and every folder", run: func() { w.foldAll(true) }},
		{title: "Unfold all", hint: "Every group and every folder", run: func() { w.foldAll(false) }},
		{title: "Show one file at a time", run: func() { w.setFileView(fileViewOne) }},
		{title: "Show all files", run: func() { w.setFileView(fileViewStack) }},
		{title: "Toggle diff layout", hint: layout, run: toggleLayout},
		{title: "Toggle word wrap", hint: wrap, keys: "⌥Z", run: toggleWrap},
		{title: "Toggle whitespace", hint: whitespace, run: func() {
			cfg.Update(func(s *Settings) { s.ShowWhitespace = !s.ShowWhitespace })
		}},
		{title: "Increase code font size", keys: "⌘+", run: func() { changeFontSize(1) }},
		{title: "Decrease code font size", keys: "⌘-", run: func() { changeFontSize(-1) }},
		{title: "Reset code font size", keys: "⌘0", run: func() { changeFontSize(0) }},
		{title: "Settings", hint: textSettingsSoon, soon: true},
		{title: "Open config file", run: openConfig},
		{title: "Refresh changes", keys: "⌘R", run: w.refresh},
		{title: "Keyboard shortcuts", keys: "⇧?", run: func() { w.help = true }},
	}
}

func toggleLayout() {
	cfg.Update(func(s *Settings) {
		if s.DiffStyle == "split" {
			s.DiffStyle = "unified"
		} else {
			s.DiffStyle = "split"
		}
	})
}

func toggleWrap() { cfg.Update(func(s *Settings) { s.WordWrap = !s.WordWrap }) }

// changeFontSize makes the code larger or smaller, or resets it with 0.
func changeFontSize(delta int) {
	cfg.Update(func(s *Settings) {
		if delta == 0 {
			s.CodeFontSize = defaultSettings().CodeFontSize
		} else {
			s.CodeFontSize += delta
		}
	})
}

func openConfig() {
	path := cfg.ensure()
	go func() {
		if err := openEditor(cfg.Get().EditorCommand, "", path, 0); err != nil {
			mygo.Dialog.Error("Could not open the config file", err.Error())
		}
	}()
}

func (w *window) focusFilter() {
	w.sidebarShown = true
	w.tab = 0
	w.filterFocus = true
}

func (w *window) openCurrent() {
	if w.current >= 0 && w.current < len(w.files) {
		f := w.files[w.current]
		w.openInEditor(f.Path, firstLine(f))
	}
}

func (w *window) copyComments() {
	md := w.commentsMarkdown()
	if md == "" {
		return
	}
	if w.win != nil {
		mygo.Clipboard.WriteText(md)
	}
	w.copied = md
	w.copiedAt = w.now
}

// palette shows the command bar while it is open.
func (w *window) palette(c *ui.Context) {
	if !w.paletteOpen {
		w.paletteQuery = ""
		return
	}
	t := c.Theme()
	pal := paletteFor(t)
	q := strings.TrimSpace(w.paletteQuery)
	var cmds []command
	for _, cmd := range w.commands() {
		// A command that is not there yet shows, so that it is no secret:
		// it is dimmed, and does nothing.
		if (cmd.run != nil || cmd.soon) && fuzzyMatch(cmd.title, q) {
			cmds = append(cmds, cmd)
		}
	}
	if w.paletteRow >= len(cmds) {
		w.paletteRow = len(cmds) - 1
	}
	if w.paletteRow < 0 && len(cmds) > 0 {
		w.paletteRow = 0
	}
	run := func(i int) {
		if i < 0 || i >= len(cmds) || cmds[i].soon {
			return
		}
		w.paletteOpen = false
		w.paletteQuery = ""
		cmds[i].run()
	}
	ui.DialogBase(c, &w.paletteOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.12)).Justify(ui.Start).Padding(120, 0, 0, 0)
		panel.Width(560).MaxHeight(440).Radius(16).Background(pal.surface).Border(1, pal.border).
			Shadow(0, 20, 60, 0, ui.RGBA(0, 0, 0, 0.28)).Clip().Label("Command bar")
		var field ui.Element
		search := ui.Row(c).Padding(4, 10).Gap(10).Radius(8).BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
			ui.Icon(c, iconCommand).FontSize(16).TextColor(t.TextMuted)
			input := ui.TextInputBase(c, &w.paletteQuery).Placeholder("Type a command…").Label("Command").FontSize(15).Grow(1).AutoFocus()
			field = input
			if input.Changed() {
				w.paletteRow = 0
			}
			if input.Shortcut(0, ui.KeyDown) && len(cmds) > 0 {
				w.paletteRow = (w.paletteRow + 1) % len(cmds)
				w.paletteList.ScrollIntoView(w.paletteRow)
			}
			if input.Shortcut(0, ui.KeyUp) && len(cmds) > 0 {
				w.paletteRow = (w.paletteRow - 1 + len(cmds)) % len(cmds)
				w.paletteList.ScrollIntoView(w.paletteRow)
			}
			if input.Submitted() {
				run(w.paletteRow)
			}
		})
		// The field the keys type into takes the review's ring, as every
		// place they go does.
		focusRingWhen(search, field.FocusVisible(), pal, 8)
		w.paletteList.Key = func(i int) any { return cmds[i].title }
		ui.List(c, &w.paletteList, len(cmds), func(i int) {
			cmd := cmds[i]
			item := ui.Row(c).MinHeight(34).Padding(6, 10).Gap(8).Radius(8).Cursor(ui.CursorPointer)
			if !cmd.soon && item.Hovered() && w.paletteMoved(item) {
				w.paletteRow = i
			}
			if i == w.paletteRow && !cmd.soon {
				item.Background(t.Accent).TextColor(t.AccentText)
			}
			if item.Clicked() {
				run(i)
			}
			item.Children(func() {
				title := ui.Text(c, cmd.title).FontSize(13).Shrink(0)
				if cmd.soon {
					title.TextColor(t.TextMuted)
				}
				if cmd.hint != "" {
					hint := ui.Text(c, cmd.hint).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
					switch {
					case cmd.soon, i != w.paletteRow:
						hint.TextColor(t.TextMuted)
					default:
						hint.Opacity(0.8)
					}
				}
				ui.Spacer(c)
				if cmd.keys != "" {
					k := ui.Text(c, cmd.keys).FontSize(11).Padding(2, 6).Radius(5).Shrink(0)
					if i == w.paletteRow {
						k.Background(ui.RGBA(255, 255, 255, 0.2))
					} else {
						k.Background(pal.pill).TextColor(t.TextMuted)
					}
				}
			})
		}).Padding(6).MaxHeight(380).Children(func() {
			if len(cmds) == 0 {
				emptyState(c, pal, emptyBox{shape: emptySmall, icon: iconCommand, title: textNoCommands, detail: q})
			}
		})
	})
	if !w.paletteOpen {
		w.paletteQuery = ""
	}
}

// paletteMoved reports whether the pointer moved over an item, so that a
// list scrolled under a still pointer keeps the row the keys chose.
func (w *window) paletteMoved(e ui.Element) bool {
	x, y, over := e.PointerPosition()
	if !over {
		return false
	}
	moved := x != w.palettePointer[0] || y != w.palettePointer[1]
	w.palettePointer = [2]float32{x, y}
	return moved
}

// dialogKind is what the open dialog asks for.
type dialogKind uint8

const (
	dialogNone dialogKind = iota
	dialogCommit
	dialogBranch
	dialogPull
)

func (w *window) openDialog(k dialogKind) {
	w.dialog = k
	w.dialogOpen = true
	w.dialogValue = ""
	w.dialogErr = ""
}

// sourceDialog asks for a commit or a branch to review.
func (w *window) sourceDialog(c *ui.Context) {
	t := c.Theme()
	if w.dialog == dialogNone {
		return
	}
	title, desc, label, placeholder := "Open Commit", "Review a commit by SHA or revision, such as HEAD~1.", "Commit", "HEAD~1 or a commit SHA"
	switch w.dialog {
	case dialogBranch:
		title, desc, label, placeholder = "Open Branch", "Compare the current working tree with a branch.", "Branch name", "main"
	case dialogPull:
		title, desc, label, placeholder = "Open Pull Request", "Review a pull request of this repository on GitHub, open or not.", "Pull request", "123, owner/repo#123 or its URL"
	}
	open := func() {
		v := strings.TrimSpace(w.dialogValue)
		if v == "" {
			w.dialogErr = "Enter a " + strings.ToLower(label) + "."
			return
		}
		if w.dialogBusy {
			return
		}
		// git answers off the main thread.
		kind := w.dialog
		w.dialogBusy = true
		w.background(func() {
			var hash string
			var err error
			if kind == dialogPull {
				hash, err = resolvePull(w.repo, v)
			} else {
				hash, err = w.repo.Resolve(v)
			}
			w.update(func() {
				w.dialogBusy = false
				if !w.dialogOpen || w.dialog != kind {
					return
				}
				switch {
				case err != nil && kind == dialogBranch:
					w.dialogErr = "Branch \"" + v + "\" does not exist in this repository."
				case err != nil:
					w.dialogErr = err.Error()
				case kind == dialogPull:
					w.dialogOpen = false
					w.setSource(source{kind: sourcePull, ref: hash})
				case kind == dialogCommit:
					w.dialogOpen = false
					w.setSource(source{kind: sourceCommit, ref: hash})
				default:
					w.dialogOpen = false
					w.setSource(source{kind: sourceBranch, ref: v})
				}
			})
		})
	}
	ui.Modal(c, &w.dialogOpen, func() {
		ui.Column(c).Width(420).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(16).Bold()
				ui.Text(c, desc).FontSize(13).TextColor(t.TextMuted)
			})
			ui.Field(c, label, func() {
				if ui.TextInput(c, &w.dialogValue).Placeholder(placeholder).Font(w.codeFont()).AutoFocus().Submitted() {
					open()
				}
			}).Error(w.dialogErr)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					w.dialogOpen = false
				}
				label := "Open"
				if w.dialogBusy {
					label = "Opening…"
				}
				if ui.PrimaryButton(c, label).Disabled(w.dialogBusy).Clicked() {
					open()
				}
			})
		})
	})
	if !w.dialogOpen {
		w.dialog = dialogNone
	}
}

// shortcutsHelp lists the keyboard shortcuts.
func (w *window) shortcutsHelp(c *ui.Context) {
	t := c.Theme()
	// One file at a time, n steps through the files not viewed yet, and the
	// notes take ⌥n; with every file stacked, n stays the notes'.
	keys := [][2]string{{"Next unviewed file", "N"}, {"Previous unviewed file", "⇧N"},
		{"Next AI note", "⌥N"}, {"Previous AI note", "⌥⇧N"}}
	if !w.oneFile() {
		keys = [][2]string{{"Next AI note", "N"}, {"Previous AI note", "⇧N"}}
	}
	navigation := [][2]string{{"Command bar", "⌘K"}, {"Filter files", "⌘P"}, {"Next hunk", "J"}, {"Previous hunk", "K"}}
	navigation = append(navigation, keys...)
	navigation = append(navigation, [2]string{"Toggle sidebar", "⌘⇧B"}, [2]string{"Toggle word wrap", "⌥Z"},
		[2]string{"Open file in editor", "⌘⇧O"}, [2]string{"Refresh changes", "⌘R"})
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{"Navigation", navigation},
		{"Search", [][2]string{{"Find in diffs", "⌘F"}, {"Next match", "↩"}, {"Previous match", "⇧↩"}, {"Close search", "Esc"}}},
		{"Comments", [][2]string{{"Comment on a line", "Click"}, {"Comment on the hunk", "↩"}, {"Add comment", "⌘↩"}, {"Discard comment", "Esc"}}},
		{"Pull requests", [][2]string{{"Pull requests", "⌘3"}, {"Review with AI", "⌘⇧I"}, {"Add to your review", "⌘↩"}, {"Reply", "⌘↩"}}},
		{"Code", [][2]string{{"Bigger text", "⌘+"}, {"Smaller text", "⌘-"}, {"Actual size", "⌘0"}}},
	}
	ui.Modal(c, &w.help, func() {
		ui.Column(c).Width(620).Gap(16).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "Keyboard Shortcuts").FontSize(15).Bold().Grow(1)
				if ui.Button(c, "Done").Clicked() {
					w.help = false
				}
			})
			ui.Grid(c).Columns(2).GapX(28).GapY(14).Children(func() {
				for _, g := range groups {
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, strings.ToUpper(g.title)).FontSize(11).FontWeight(600).LetterSpacing(0.6).TextColor(t.TextMuted)
						for _, k := range g.keys {
							ui.Row(c).MinHeight(26).Children(func() {
								ui.Text(c, k[0]).FontSize(13).Grow(1)
								ui.Text(c, k[1]).FontSize(11).Padding(2, 6).Radius(5).Background(paletteFor(t).pill)
							})
						}
					})
				}
			})
		})
	})
}

// showPulls shows the pull requests in the sidebar.
func (w *window) showPulls() {
	if !w.gh.ok() {
		w.openDialog(dialogPull)
		return
	}
	w.tab, w.sidebarShown = 2, true
}

// openSubmit opens the dialog submitting the review of the pull request
// shown.
func (w *window) openSubmit() {
	if p := w.pr; p != nil && p.canWrite() {
		p.submitOpen = true
		p.submitErr = ""
		if p.submitEvent == "" {
			p.submitEvent = github.VerdictComment
		}
	}
}

func (w *window) openPullOnGitHub() {
	if w.pr != nil && w.pr.meta != nil && w.win != nil {
		mygo.Shell.OpenExternal(w.pr.meta.HTMLURL)
	}
}

// agentHint names the agent that reviews, and the model chosen.
func (w *window) agentHint() string {
	a, ok := agent.ByName(w.settings.AIAgent)
	if !ok {
		return "Claude Code, Codex, OpenCode or Pi"
	}
	if w.settings.AIModel != "" {
		return a.Label + " · " + w.settings.AIModel
	}
	return a.Label
}

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
	layout := msg().SwitchToUnified()
	if !w.split() {
		layout = msg().SwitchToSplit()
	}
	wrap := msg().TurnWrapOn()
	if w.settings.WordWrap {
		wrap = msg().TurnWrapOff()
	}
	// Going to the notes of the agent's review, when there are.
	notes := func(dir int) func() {
		if len(w.notes) == 0 {
			return nil
		}
		return func() { w.nextNote(dir) }
	}
	whitespace := msg().ShowWhitespaceChanges()
	if w.settings.ShowWhitespace {
		whitespace = msg().HideWhitespaceChanges()
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
	stepNext, stepPrev := "", ""
	if w.oneFile() {
		unviewedKeys, noteKeys, noteBack = "N", "⌥N", "⌥⇧N"
		stepNext, stepPrev = "]", "["
	}
	return []command{
		{title: msg().FocusFileFilter(), keys: "⌘P", run: w.focusFilter},
		{title: msg().ToolbarFind(), keys: "⌘F", run: func() { w.finding = true }},
		{title: msg().OpenCommitCommand(), hint: msg().OpenCommitHint(), run: func() { w.openDialog(dialogCommit) }},
		{title: msg().OpenBranchCommand(), hint: msg().OpenBranchHint(), run: func() { w.openDialog(dialogBranch) }},
		{title: msg().OpenPullCommand(), hint: msg().OpenPullHint(), run: func() { w.openDialog(dialogPull) }},
		{title: msg().OpenFolder(), keys: "⌘O", run: openFolder},
		{title: msg().SettingsCommand(), keys: "⌘,", run: openSettings},
		{title: msg().ShowFileTree(), keys: "⌘1", run: func() { w.tab, w.sidebarShown = 0, true }},
		{title: msg().ShowHistoryCommand(), keys: "⌘2", run: func() { w.tab, w.sidebarShown = 1, true }},
		{title: msg().ShowPullRequestsCommand(), keys: "⌘3", run: w.showPulls},
		{title: msg().NextUnviewed(), hint: msg().NextUnviewedHint(), keys: unviewedKeys, run: func() { w.stepUnviewed(1) }},
		{title: textReviewWithAI(), hint: w.agentHint(), keys: "⌘⇧I", run: w.reviewWithAI},
		{title: msg().StopAiReview(), run: w.stopAnalysis},
		{title: msg().NextAiNoteCommand(), keys: noteKeys, run: notes(1)},
		{title: msg().PreviousAiNoteCommand(), keys: noteBack, run: notes(-1)},
		{title: msg().GroupByKindCommand(), run: func() { w.chooseGrouping(groupKind) }},
		{title: msg().GroupByAICommand(), run: func() { w.chooseGrouping(groupAI) }},
		{title: msg().UngroupFiles(), run: func() { w.chooseGrouping(groupNone) }},
		{title: msg().ShowUncommittedChanges(), run: func() { w.setSource(w.launchWorkTree()) }},
		{title: msg().CommitCommand(), run: func() {
			if w.source.kind == sourceWorkingTree && !w.commitOpen {
				w.toggleCommit()
			}
		}},
		{title: msg().CopyComments(), run: w.copyComments},
		{title: msg().ExportReviewTask(), hint: msg().ExportReviewTaskHint(), run: exportTask},
		{title: msg().FixCommentsCommand(), hint: w.fixHint(), run: fixComments},
		{title: msg().SubmitReview(), hint: msg().SubmitReviewHint(), run: w.openSubmit},
		{title: msg().OpenPullOnGitHub(), run: w.openPullOnGitHub},
		{title: msg().CopyCommentsAndClose(), run: func() {
			w.copyComments()
			if w.win != nil {
				w.win.Close()
			}
		}},
		{title: msg().ToggleViewed(), hint: selected, run: func() {
			if w.current >= 0 && w.current < len(w.files) {
				f := w.files[w.current]
				w.setViewed(f, !w.isViewed(f))
			}
		}},
		// The card's other actions have keys and menus of their own: the file
		// opens in the editor with ⌘⇧O, and its path is copied here, as a
		// right-click on its header copies it.
		{title: msg().CopyPath(), hint: selected, run: func() {
			if p := w.shownPath(); p != "" && w.win != nil {
				mygo.Clipboard.WriteText(p)
			}
		}},
		{title: msg().MarkAllViewed(), hint: msg().MarkAllViewedHint(), run: w.markAllViewed},
		{title: msg().ResetReviewState(), hint: msg().ResetReviewStateHint(), run: w.resetReviewState},
		{title: msg().NextFileCommand(), hint: msg().OneFileHint(), keys: stepNext, run: func() { w.stepFile(1) }},
		{title: msg().PreviousFileCommand(), hint: msg().OneFileHint(), keys: stepPrev, run: func() { w.stepFile(-1) }},
		{title: msg().OpenFileInEditorCommand(), hint: selected, keys: "⌘⇧O", run: w.openCurrent},
		{title: msg().ToggleSidebarMenu(), keys: "⌘⇧B", run: w.toggleSidebar},
		{title: msg().FoldAllCommand(), hint: msg().EveryGroupAndFolder(), run: func() { w.foldAll(true) }},
		{title: msg().UnfoldAllCommand(), hint: msg().EveryGroupAndFolder(), run: func() { w.foldAll(false) }},
		{title: msg().ShowOneFile(), run: func() { w.setFileView(fileViewOne) }},
		{title: msg().ShowAllFiles(), run: func() { w.setFileView(fileViewStack) }},
		{title: msg().ToggleDiffLayout(), hint: layout, run: toggleLayout},
		{title: msg().ToggleWordWrap(), hint: wrap, keys: "⌥Z", run: toggleWrap},
		{title: msg().ToggleWhitespace(), hint: whitespace, run: func() {
			cfg.Update(func(s *Settings) { s.ShowWhitespace = !s.ShowWhitespace })
		}},
		{title: msg().IncreaseFontSize(), keys: "⌘+", run: func() { changeFontSize(1) }},
		{title: msg().DecreaseFontSize(), keys: "⌘-", run: func() { changeFontSize(-1) }},
		{title: msg().ResetFontSize(), keys: "⌘0", run: func() { changeFontSize(0) }},
		{title: msg().SettingsTitle(), hint: textSettingsSoon(), soon: true},
		{title: msg().OpenConfigCommand(), run: openConfig},
		{title: msg().RefreshChanges(), keys: "⌘R", run: w.refresh},
		{title: msg().KeyboardShortcutsCommand(), keys: "⇧?", run: func() { w.help = true }},
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
			mygo.Dialog.Error(msg().CouldNotOpenConfigFile(), err.Error())
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
			Shadow(0, 20, 60, 0, ui.RGBA(0, 0, 0, 0.28)).Clip().Label(msg().CommandBar())
		var field ui.Element
		search := ui.Row(c).Padding(4, 10).Gap(10).Radius(8).BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
			uiIcon(c, iconCommand, iconBase).TextColor(t.TextMuted)
			input := ui.TextInputBase(c, &w.paletteQuery).Placeholder(msg().TypeCommand()).Label(msg().CommandLabel()).FontSize(15).Grow(1).AutoFocus()
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
				emptyState(c, pal, emptyBox{shape: emptySmall, icon: iconCommand, title: textNoCommands(), detail: q})
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
	title, desc, label, placeholder := msg().OpenCommitTitle(), msg().OpenCommitDesc(), msg().CommitField(), msg().CommitPlaceholder()
	switch w.dialog {
	case dialogBranch:
		title, desc, label, placeholder = msg().OpenBranchTitle(), msg().OpenBranchDesc(), msg().BranchField(), msg().BranchPlaceholder()
	case dialogPull:
		title, desc, label, placeholder = msg().OpenPullTitle(), msg().OpenPullDesc(), msg().PullField(), msg().PullPlaceholder()
	}
	open := func() {
		v := strings.TrimSpace(w.dialogValue)
		if v == "" {
			w.dialogErr = msg().EnterField(strings.ToLower(label))
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
					w.dialogErr = msg().BranchMissing(v)
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
				if ui.Button(c, msg().Cancel()).Clicked() {
					w.dialogOpen = false
				}
				label := msg().OpenAction()
				if w.dialogBusy {
					label = msg().Opening()
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
	keys := [][2]string{{msg().NextUnviewed(), "N"}, {msg().PreviousUnviewed(), "⇧N"},
		{msg().NextAiNoteCommand(), "⌥N"}, {msg().PreviousAiNoteCommand(), "⌥⇧N"}}
	if !w.oneFile() {
		keys = [][2]string{{msg().NextAiNoteCommand(), "N"}, {msg().PreviousAiNoteCommand(), "⇧N"}}
	}
	navigation := [][2]string{{msg().CommandBar(), "⌘K"}, {msg().FilterFiles(), "⌘P"}, {msg().NextHunk(), "J"}, {msg().PreviousHunk(), "K"},
		{msg().ToggleViewed(), "V"}}
	navigation = append(navigation, keys...)
	navigation = append(navigation, [2]string{msg().ToggleSidebarMenu(), "⌘⇧B"}, [2]string{msg().ToggleWordWrap(), "⌥Z"},
		[2]string{msg().OpenFileInEditorCommand(), "⌘⇧O"}, [2]string{msg().RefreshChanges(), "⌘R"})
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{msg().ShortcutsNavigation(), navigation},
		{msg().ShortcutsSearch(), [][2]string{{msg().ToolbarFind(), "⌘F"}, {msg().NextMatch(), "↩"}, {msg().PreviousMatch(), "⇧↩"}, {msg().CloseSearch(), "Esc"}}},
		{msg().ShortcutsComments(), [][2]string{{msg().CommentOnLine(), msg().ClickAction()}, {msg().CommentOnHunk(), "↩"}, {msg().AddComment(), "⌘↩"}, {msg().DiscardComment(), "Esc"}}},
		{msg().ShortcutsPullRequests(), [][2]string{{msg().PullRequestsLabel(), "⌘3"}, {textReviewWithAI(), "⌘⇧I"}, {msg().AddToYourReview(), "⌘↩"}, {msg().ReplyAction(), "⌘↩"}}},
		{msg().ShortcutsCode(), [][2]string{{msg().BiggerText(), "⌘+"}, {msg().SmallerText(), "⌘-"}, {msg().ActualSizeText(), "⌘0"}}},
	}
	ui.Modal(c, &w.help, func() {
		ui.Column(c).Width(620).Gap(16).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, msg().KeyboardShortcuts()).FontSize(15).Bold().Grow(1)
				if ui.Button(c, msg().DoneAction()).Clicked() {
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

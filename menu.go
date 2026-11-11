package main

import (
	"os/exec"
	"runtime"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/proc"
	"github.com/egoist/mygo"
)

// focused returns the review window with the focus, nil for none.
func focused(win *mygo.Window) *window {
	windowsMu.Lock()
	defer windowsMu.Unlock()
	for _, w := range windows {
		if w.win == win {
			return w
		}
	}
	return nil
}

// inWindow returns a menu item's action on the focused review window.
func inWindow(fn func(w *window)) func(*mygo.MenuItem, *mygo.Window) {
	return func(_ *mygo.MenuItem, win *mygo.Window) {
		if w := focused(win); w != nil {
			w.win.Update(func() { fn(w) })
		}
	}
}

// setting returns a menu item's action on the settings: it changes one, and
// lays the rows of the windows out again, as the review's switches decide what
// the surface shows.
func setting(fn func(s *Settings)) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) {
		cfg.Update(fn)
		windowsMu.Lock()
		defer windowsMu.Unlock()
		for _, w := range windows {
			if w.win == nil {
				w.rowsDirty = true
				continue
			}
			w.win.Update(func() { w.rowsDirty = true })
		}
	}
}

// summaryLocales are the languages a review may be asked for: the interface's
// own first, then the tags the settings keep.
var summaryLocales = []string{"", "en", "zh-CN", "ja", "de", "fr"}

// summaryLocaleLabel names a language of the review's.
func summaryLocaleLabel(tag string) string {
	switch tag {
	case "":
		return msg().ReaderLanguage()
	case "en":
		return "English"
	case "zh-CN":
		return "简体中文"
	case "ja":
		return "日本語"
	case "de":
		return "Deutsch"
	case "fr":
		return "Français"
	}
	return tag
}

var appMenu *mygo.Menu

// buildMenu makes the menu bar.
func buildMenu() *mygo.Menu {
	s := cfg.Get()
	setTheme := func(theme string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { cfg.Update(func(s *Settings) { s.Theme = theme }) }
	}
	appMenu = mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{ID: "settings", Label: msg().SettingsMenuItem(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { openSettings() }},
			{ID: "config-file", Label: msg().ConfigFile(), Click: func(*mygo.MenuItem, *mygo.Window) { openConfig() }},
			{Label: msg().SettingsInstallCLI(), Hidden: runtime.GOOS == "windows", Click: func(*mygo.MenuItem, *mygo.Window) { installCLI() }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: msg().FileMenu(), Submenu: []*mygo.MenuItem{
			{Label: msg().OpenFolder(), Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { openFolder() }},
			{Label: msg().OpenRecentMenu(), Submenu: recentMenuItems()},
			{Label: msg().SwitchRepository(), Click: func(*mygo.MenuItem, *mygo.Window) { go showWelcome(nil) }},
			{Label: msg().OpenCommitMenu(), Accelerator: "CmdOrCtrl+Shift+C", Click: inWindow(func(w *window) { w.openDialog(dialogCommit) })},
			{Label: msg().OpenBranchMenu(), Click: inWindow(func(w *window) { w.openDialog(dialogBranch) })},
			{Label: msg().OpenPullMenu(), Click: inWindow(func(w *window) { w.openDialog(dialogPull) })},
			mygo.Separator(),
			{Label: msg().CommitMenu(), Accelerator: "CmdOrCtrl+Shift+Enter", Click: inWindow(func(w *window) {
				if w.source.kind == sourceWorkingTree && !w.commitOpen && len(w.files) > 0 {
					w.toggleCommit()
				}
			})},
			mygo.Separator(),
			{Label: msg().OpenFileInEditor(), Accelerator: "CmdOrCtrl+Shift+O", Click: inWindow(func(w *window) { w.openCurrent() })},
			mygo.Separator(),
			{Label: msg().CloseRepository(), Accelerator: "CmdOrCtrl+Shift+W", Click: inWindow(func(w *window) { closeRepository(w) })},
			{Role: mygo.RoleClose},
		}},
		{Label: msg().EditMenu(), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo},
			{Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut},
			{Role: mygo.RoleCopy},
			{Role: mygo.RolePaste},
			{Role: mygo.RoleSelectAll},
			mygo.Separator(),
			{Label: msg().FindInDiffsMenu(), Accelerator: "CmdOrCtrl+F", Click: inWindow(func(w *window) { w.finding = true })},
			{Label: msg().FilterFilesMenu(), Accelerator: "CmdOrCtrl+P", Click: inWindow(func(w *window) { w.focusFilter() })},
			mygo.Separator(),
			{Label: msg().CopyReviewCommentsMenu(), Accelerator: "CmdOrCtrl+Shift+M", Click: inWindow(func(w *window) { w.copyComments() })},
		}},
		{Label: msg().ViewMenu(), Submenu: []*mygo.MenuItem{
			{Label: msg().CommandBarMenu(), Accelerator: "CmdOrCtrl+K", Click: inWindow(func(w *window) { w.paletteOpen = !w.paletteOpen })},
			{Label: msg().ToggleSidebarMenu(), Accelerator: "CmdOrCtrl+Shift+B", Click: inWindow(func(w *window) { w.toggleSidebar() })},
			{Label: msg().FilesTabMenu(), Accelerator: "CmdOrCtrl+1", Click: inWindow(func(w *window) { w.tab, w.sidebarShown = 0, true })},
			{Label: msg().HistoryTabMenu(), Accelerator: "CmdOrCtrl+2", Click: inWindow(func(w *window) { w.tab, w.sidebarShown = 1, true })},
			{Label: msg().PullRequestsTabMenu(), Accelerator: "CmdOrCtrl+3", Click: inWindow(func(w *window) { w.showPulls() })},
			mygo.Separator(),
			{Label: textReviewWithAI(), Accelerator: "CmdOrCtrl+Shift+I", Click: inWindow(func(w *window) { w.reviewWithAI() })},
			{Label: textGroup(), Submenu: []*mygo.MenuItem{
				{Label: textNoGroup(), Click: inWindow(func(w *window) { w.chooseGrouping(groupNone) })},
				{Label: textByKind(), Click: inWindow(func(w *window) { w.chooseGrouping(groupKind) })},
				{Label: textByAI(), Click: inWindow(func(w *window) { w.chooseGrouping(groupAI) })},
			}},
			{Label: msg().ViewOneFile(), Submenu: []*mygo.MenuItem{
				{ID: "fileview-one", Label: textOneFile(), Type: mygo.MenuItemRadio, Checked: s.FileView != fileViewStack,
					Click: setting(func(s *Settings) { s.FileView = fileViewOne })},
				{ID: "fileview-stack", Label: textStack(), Type: mygo.MenuItemRadio, Checked: s.FileView == fileViewStack,
					Click: setting(func(s *Settings) { s.FileView = fileViewStack })},
			}},
			{Label: msg().AiAgentMenu(), Submenu: agentMenuItems(s)},
			{Label: msg().AiReviewMenu(), Submenu: reviewMenuItems(s)},
			mygo.Separator(),
			{Label: msg().DiffMenu(), Submenu: []*mygo.MenuItem{
				{ID: "split", Label: textSplit(), Type: mygo.MenuItemRadio, Checked: s.DiffStyle == "split", Click: func(*mygo.MenuItem, *mygo.Window) {
					cfg.Update(func(s *Settings) { s.DiffStyle = "split" })
				}},
				{ID: "unified", Label: textUnified(), Type: mygo.MenuItemRadio, Checked: s.DiffStyle == "unified", Click: func(*mygo.MenuItem, *mygo.Window) {
					cfg.Update(func(s *Settings) { s.DiffStyle = "unified" })
				}},
				mygo.Separator(),
				{ID: "wrap", Label: textWrap(), Type: mygo.MenuItemCheckbox, Checked: s.WordWrap, Click: func(*mygo.MenuItem, *mygo.Window) { toggleWrap() }},
				{ID: "whitespace", Label: textSpace(), Type: mygo.MenuItemCheckbox, Checked: s.ShowWhitespace, Click: func(*mygo.MenuItem, *mygo.Window) {
					cfg.Update(func(s *Settings) { s.ShowWhitespace = !s.ShowWhitespace })
				}},
				mygo.Separator(),
				{Label: msg().CollapseAllFilesMenu(), Click: inWindow(func(w *window) { w.setAllCollapsed(true) })},
				{Label: msg().ExpandAllFilesMenu(), Click: inWindow(func(w *window) { w.setAllCollapsed(false) })},
			}},
			{Label: msg().FontSizeMenu(), Submenu: []*mygo.MenuItem{
				{Label: msg().BiggerText(), Accelerator: "CmdOrCtrl+=", Click: func(*mygo.MenuItem, *mygo.Window) { changeFontSize(1) }},
				{Label: msg().SmallerText(), Accelerator: "CmdOrCtrl+-", Click: func(*mygo.MenuItem, *mygo.Window) { changeFontSize(-1) }},
				{Label: msg().ActualSizeText(), Accelerator: "CmdOrCtrl+0", Click: func(*mygo.MenuItem, *mygo.Window) { changeFontSize(0) }},
			}},
			{Label: msg().CommentsMenu(), Submenu: []*mygo.MenuItem{
				{ID: "copyOnClose", Label: msg().CopyCommentsOnClose(), Type: mygo.MenuItemCheckbox, Checked: s.CopyCommentsOnClose, Click: func(*mygo.MenuItem, *mygo.Window) {
					cfg.Update(func(s *Settings) { s.CopyCommentsOnClose = !s.CopyCommentsOnClose })
				}},
			}},
			{Label: msg().ThemeMenu(), Submenu: []*mygo.MenuItem{
				{ID: "theme-system", Label: textSystem(), Type: mygo.MenuItemRadio, Checked: s.Theme == "system", Click: setTheme("system")},
				{ID: "theme-light", Label: textLight(), Type: mygo.MenuItemRadio, Checked: s.Theme == "light", Click: setTheme("light")},
				{ID: "theme-dark", Label: textDark(), Type: mygo.MenuItemRadio, Checked: s.Theme == "dark", Click: setTheme("dark")},
			}},
			{Label: msg().LanguageMenu(), Submenu: languageMenuItems(s)},
			mygo.Separator(),
			{ID: "settings-view", Label: msg().SettingsMenuItem(), Click: func(*mygo.MenuItem, *mygo.Window) { openSettings() }},
			mygo.Separator(),
			{Label: msg().RefreshChangesMenu(), Accelerator: "CmdOrCtrl+R", Click: inWindow(func(w *window) { w.refresh() })},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp, Submenu: []*mygo.MenuItem{
			{Label: msg().KeyboardShortcuts(), Click: inWindow(func(w *window) { w.help = true })},
		}},
	})
	return appMenu
}

// syncMenu shows the settings in the menu's check marks.
func syncMenu(s Settings) {
	if appMenu == nil {
		return
	}
	set := func(id string, on bool) {
		if it := appMenu.ItemByID(id); it != nil {
			it.SetChecked(on)
		}
	}
	set("split", s.DiffStyle == "split")
	set("unified", s.DiffStyle == "unified")
	set("wrap", s.WordWrap)
	set("whitespace", s.ShowWhitespace)
	set("copyOnClose", s.CopyCommentsOnClose)
	set("fileview-one", s.FileView != fileViewStack)
	set("fileview-stack", s.FileView == fileViewStack)
	set("grouping", s.GroupingEnabled)
	set("overallSummary", s.OverallSummary)
	set("summaryExpanded", s.SummaryExpanded)
	set("notes", s.Notes)
	for _, tag := range summaryLocales {
		set("locale-"+tag, s.SummaryLocale == tag)
	}
	set("uilocale-", s.UILocale == "")
	for _, tag := range uiLanguages {
		set("uilocale-"+tag, s.UILocale == tag)
	}
	set("theme-system", s.Theme == "system")
	set("theme-light", s.Theme == "light")
	set("theme-dark", s.Theme == "dark")
	set("agent-", s.AIAgent == "")
	for _, a := range agent.All {
		set("agent-"+a.Name, s.AIAgent == a.Name)
	}
}

// applyTheme follows the theme of the settings.
func applyTheme(s Settings) {
	switch s.Theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// openFolder asks for a repository and opens it.
func openFolder() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: msg().OpenRepoTitle(), Directory: true})
		if err != nil || len(paths) == 0 {
			return
		}
		if err := openWindow(paths[0], source{kind: sourceWorkingTree}); err != nil {
			mygo.Dialog.Error(msg().CouldNotOpenFolder(), errorText(err))
			return
		}
		closeWelcome()
	}()
}

// recentMenuItems open the repositories opened lately, as File → Open
// Recent.
func recentMenuItems() []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, root := range state.recent() {
		items = append(items, &mygo.MenuItem{Label: abbreviateHome(root), ToolTip: root, Click: func(*mygo.MenuItem, *mygo.Window) {
			go openRecent(root)
		}})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	return append(items, &mygo.MenuItem{Label: msg().ClearRecentMenu(), Disabled: len(items) == 0, Click: func(*mygo.MenuItem, *mygo.Window) {
		state.clearRecent()
		mygo.App.SetMenu(buildMenu())
	}})
}

// closeRepository closes the window of a repository and opens the welcome
// page: a window is a repository, so the way to another one leads back there
// rather than to a window that shows none.
func closeRepository(w *window) {
	win := w.win
	go func() {
		win.Close()
		showWelcome(nil)
	}()
}

// revealInFinder shows a folder in the Finder, on the platforms that have
// one.
func revealInFinder(path string) {
	if runtime.GOOS != "darwin" {
		return
	}
	cmd := exec.Command("open", "-R", path)
	proc.HideConsole(cmd)
	cmd.Run()
}

// openRecent opens a repository of File → Open Recent.
func openRecent(root string) {
	if err := openWindow(root, source{kind: sourceWorkingTree}); err != nil {
		mygo.Dialog.Error(msg().CouldNotOpen(abbreviateHome(root)), errorText(err))
		return
	}
	closeWelcome()
}

// noteRecent puts a repository first in File → Open Recent.
func noteRecent(root string) {
	if state.addRecent(root, cfg.Get().MaxRecent) {
		mygo.RunOnMain(func() { mygo.App.SetMenu(buildMenu()) })
	}
}

// agentMenuItems choose the agent that reviews with AI.
func agentMenuItems(s Settings) []*mygo.MenuItem {
	choose := func(name string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			cfg.Update(func(s *Settings) {
				if s.AIAgent != name {
					s.AIAgent, s.AIModel = name, ""
				}
			})
		}
	}
	items := []*mygo.MenuItem{
		{ID: "agent-", Label: msg().Automatic(), Type: mygo.MenuItemRadio, Checked: s.AIAgent == "", Click: choose("")},
		mygo.Separator(),
	}
	for _, a := range agent.All {
		items = append(items, &mygo.MenuItem{ID: "agent-" + a.Name, Label: a.Label, Type: mygo.MenuItemRadio, Checked: s.AIAgent == a.Name, Click: choose(a.Name)})
	}
	return items
}

// reviewMenuItems are the settings of the agent's review: the switches that
// decide what it is asked for and what the surface shows.
func reviewMenuItems(s Settings) []*mygo.MenuItem {
	switchItem := func(id, label string, on bool, flip func(s *Settings)) *mygo.MenuItem {
		return &mygo.MenuItem{ID: id, Label: label, Type: mygo.MenuItemCheckbox, Checked: on, Click: setting(flip)}
	}
	locales := make([]*mygo.MenuItem, 0, len(summaryLocales))
	for _, tag := range summaryLocales {
		chosen := tag
		locales = append(locales, &mygo.MenuItem{ID: "locale-" + tag, Label: summaryLocaleLabel(tag), Type: mygo.MenuItemRadio, Checked: s.SummaryLocale == tag,
			Click: func(*mygo.MenuItem, *mygo.Window) { cfg.Update(func(s *Settings) { s.SummaryLocale = chosen }) }})
	}
	return []*mygo.MenuItem{
		switchItem("grouping", msg().SmartGrouping(), s.GroupingEnabled, func(s *Settings) { s.GroupingEnabled = !s.GroupingEnabled }),
		mygo.Separator(),
		switchItem("overallSummary", msg().OverallSummaryMenu(), s.OverallSummary, func(s *Settings) { s.OverallSummary = !s.OverallSummary }),
		switchItem("summaryExpanded", msg().ExpandSummaryMenu(), s.SummaryExpanded, func(s *Settings) { s.SummaryExpanded = !s.SummaryExpanded }),
		switchItem("notes", msg().NotesOnFilesMenu(), s.Notes, func(s *Settings) { s.Notes = !s.Notes }),
		mygo.Separator(),
		{Label: msg().SummaryLanguageMenu(), Submenu: locales},
	}
}

package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/github"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// The settings are a window of their own, not a page of a review: they belong
// to the app, they open with ⌘, from anywhere, and they outlive the repository
// a window shows. What they hold is the Settings of config.go, written as it
// changes — there is no Save button, and every window hears of a change as it
// is made.
//
// The window has two halves: the groups of settings it holds, on the left, and
// the form of the group chosen, on the right, scrolling. Every word of it comes
// from the catalogs in locales/, so that it says the language the interface
// says, and says it again when that changes.

// The groups of the settings, in the order the window lists them.
const (
	settingsAppearance = "appearance"
	settingsReview     = "review"
	settingsAI         = "ai"
	settingsGitHub     = "github"
	settingsWorkspace  = "workspace"
	settingsAdvanced   = "advanced"
)

// settingsPages are the groups, in the order the window lists them.
var settingsPages = []string{
	settingsAppearance, settingsReview, settingsAI, settingsGitHub, settingsWorkspace, settingsAdvanced,
}

// settingsWindow is the window of the settings: what it shows, and what it
// asks the app for as it shows it.
type settingsWindow struct {
	win  *mygo.Window
	page string

	// The fields of the form that are typed into, or counted, keep a copy of
	// what they hold, so that a frame does not lose the edit of the one
	// before it.
	font      string
	token     string
	model     string
	fixModel  string
	fontSize  float64
	maxFiles  float64
	maxRecent float64
	analyses  float64
	runs      float64

	// The account the token is, as last asked for.
	gh githubUser

	// What the cache takes, and what clearing it took.
	cacheBytes int64
	cacheKnown bool
	cacheBusy  bool
	cleared    string

	// confirmClear asks before the cache goes.
	confirmClear bool
}

var (
	settingsMu  sync.Mutex
	settingsWin *settingsWindow
)

// mountSettingsWindow puts the settings in a window of their own: it is a var,
// so that a test opens the settings without a window.
var mountSettingsWindow = func(sw *settingsWindow) *mygo.Window {
	return mygo.NewWindow(mygo.WindowOptions{
		Title:         sw.title(),
		Width:         720,
		Height:        560,
		MinWidth:      600,
		MinHeight:     440,
		StateKey:      "settings",
		TitleBarStyle: mygo.TitleBarHiddenInset,
		Content:       ui.View(sw.view),
	})
}

// focusSettingsWindow comes to the window of the settings: it is a var, so
// that a test sees a second ⌘, come to the window the first one opened.
var focusSettingsWindow = func(sw *settingsWindow) { sw.focus() }

// openSettings shows the settings: the window of them that is open already, or
// a new one. ⌘, twice is one window, and the second time comes to it.
func openSettings() {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if settingsWin != nil && settingsWin.alive() {
		focusSettingsWindow(settingsWin)
		return
	}
	sw := newSettingsWindow()
	settingsWin = sw
	sw.mount()
}

func newSettingsWindow() *settingsWindow {
	s := cfg.Get()
	return &settingsWindow{
		page:      settingsAppearance,
		font:      s.CodeFontFamily,
		token:     s.GithubToken,
		model:     s.AIModel,
		fixModel:  s.FixModel,
		fontSize:  float64(s.CodeFontSize),
		maxFiles:  float64(s.MaxFiles),
		maxRecent: float64(s.MaxRecent),
		analyses:  float64(s.AnalysesKept),
		runs:      float64(s.RunsKept),
	}
}

// mount opens the window of the settings and follows the settings from then
// on: a menu, the config file or this window writes them, and the form shows
// what they say.
func (sw *settingsWindow) mount() {
	sw.win = mountSettingsWindow(sw)
	if sw.win != nil {
		// A test asks the window it renders, and has none to follow.
		off := cfg.OnChange(func(s Settings) { sw.win.Update(func() { sw.refresh(s) }) })
		sw.win.OnClosed(func() {
			off()
			settingsMu.Lock()
			if settingsWin == sw {
				settingsWin = nil
			}
			settingsMu.Unlock()
		})
	}
	go sw.loadCacheSize()
	go sw.loadGitHub()
}

func (sw *settingsWindow) title() string { return msg().SettingsTitle() }

// alive reports whether the window of the settings is still there.
func (sw *settingsWindow) alive() bool { return sw.win == nil || !sw.win.IsDestroyed() }

// focus comes to the window of the settings, and to it alone.
func (sw *settingsWindow) focus() {
	if sw.win == nil {
		return
	}
	sw.win.Show()
	sw.win.Focus()
}

func (sw *settingsWindow) update(fn func()) {
	if sw.win == nil {
		fn()
		return
	}
	sw.win.Update(fn)
}

// background runs fn on a goroutine of its own, or at once in a test, which
// has no window to draw while it waits.
func (sw *settingsWindow) background(fn func()) {
	if sw.win == nil {
		fn()
		return
	}
	go fn()
}

func (sw *settingsWindow) invalidate() {
	if sw.win != nil {
		sw.win.Invalidate()
	}
}

// refresh takes the settings the app now holds, for the fields this window
// keeps a copy of: those typed into, and those counted.
func (sw *settingsWindow) refresh(s Settings) {
	if sw.font != s.CodeFontFamily {
		sw.font = s.CodeFontFamily
	}
	if sw.token != s.GithubToken {
		// The token changed: GitHub is asked whose it is when it is entered,
		// rather than with every letter typed.
		sw.token = s.GithubToken
		sw.gh = githubUser{}
	}
	sw.model, sw.fixModel = s.AIModel, s.FixModel
	sw.fontSize = float64(s.CodeFontSize)
	sw.maxFiles = float64(s.MaxFiles)
	sw.maxRecent = float64(s.MaxRecent)
	sw.analyses = float64(s.AnalysesKept)
	sw.runs = float64(s.RunsKept)
}

// retitleSettings says the window of the settings in the language the
// interface shows now, and lays its rows out again in its words.
func retitleSettings() {
	settingsMu.Lock()
	sw := settingsWin
	settingsMu.Unlock()
	if sw == nil {
		return
	}
	if sw.win != nil {
		sw.win.SetTitle(sw.title())
	}
	sw.update(func() {})
}

// invalidateSettings draws the window of the settings again, as the agents it
// lists come and go.
func invalidateSettings() {
	settingsMu.Lock()
	sw := settingsWin
	settingsMu.Unlock()
	if sw != nil {
		sw.invalidate()
	}
}

// set is a setting written from this window: the app keeps it, writes it, and
// every window, this one among them, hears of it.
func (sw *settingsWindow) set(fn func(s *Settings)) { cfg.Update(fn) }

// view draws the window: the groups of the settings down the left, the form of
// the group chosen down the right.
func (sw *settingsWindow) view(c *ui.Context) {
	pal := paletteFor(c.Theme())
	c.Root().Background(pal.appBg)
	if !slices.Contains(settingsPages, sw.page) {
		sw.page = settingsAppearance
	}
	ui.Column(c).Fill().Children(func() {
		sw.header(c, pal)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			sw.nav(c, pal)
			ui.Scroll(c).Grow(1).MinWidth(0).Background(pal.appBg).Children(func() {
				ui.Column(c).Padding(18, 26, 16, 26).Gap(gapBlock).MaxWidth(640).Children(func() {
					sw.pageView(c, pal)
				})
			})
		})
	})
	sw.clearCacheDialog(c)
}

// header is the bar along the top of the window: the name of the window, and
// the room the window controls take, which it drags the window by.
func (sw *settingsWindow) header(c *ui.Context, pal *palette) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, 44)).Padding(0, 12, 0, bar.Left+4).Gap(8).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(pal.border).DragWindow().Children(func() {
		ui.Text(c, sw.title()).FontSize(13).FontWeight(700).TextColor(t.Text)
	})
}

// nav lists the groups of the settings, each with the icon of what it holds.
func (sw *settingsWindow) nav(c *ui.Context, pal *palette) {
	ui.Column(c).Width(190).Shrink(0).Background(pal.surface).BorderWidth(0, 1, 0, 0).BorderColor(pal.border).
		Children(func() {
			ui.Sidebar(c, &sw.page, func() {
				for _, page := range settingsPages {
					ui.SidebarItem(c, page, settingsPageIcon(page), settingsPageLabel(page))
				}
			}).Grow(1)
		})
}

// settingsPageIcon is the icon of a group of the settings.
func settingsPageIcon(page string) *ui.SVG {
	switch page {
	case settingsAppearance:
		return iconPalette
	case settingsReview:
		return iconList
	case settingsAI:
		return iconSparkle
	case settingsGitHub:
		return iconPull
	case settingsWorkspace:
		return iconSidebar
	}
	return iconSliders
}

// settingsPageLabel names a group of the settings, in the language the
// interface shows.
func settingsPageLabel(page string) string {
	switch page {
	case settingsAppearance:
		return msg().SettingsAppearance()
	case settingsReview:
		return msg().SettingsReview()
	case settingsAI:
		return msg().SettingsAI()
	case settingsGitHub:
		return msg().SettingsGitHub()
	case settingsWorkspace:
		return msg().SettingsWorkspace()
	}
	return msg().SettingsAdvanced()
}

// pageView builds the form of the group chosen.
func (sw *settingsWindow) pageView(c *ui.Context, pal *palette) {
	switch sw.page {
	case settingsReview:
		sw.reviewPage(c, pal)
	case settingsAI:
		sw.aiPage(c, pal)
	case settingsGitHub:
		sw.githubPage(c, pal)
	case settingsWorkspace:
		sw.workspacePage(c, pal)
	case settingsAdvanced:
		sw.advancedPage(c, pal)
	default:
		sw.appearancePage(c, pal)
	}
}

// section is the heading of a part of a page, and the words of what it holds.
func section(c *ui.Context, title, detail string) {
	t := c.Theme()
	ui.Column(c).Gap(3).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(700).TextColor(t.TextMuted)
		if detail != "" {
			ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).Wrap().Selectable()
		}
	})
}

// form is the fields of a page, their labels in a column of their own.
func form(c *ui.Context, fn func()) {
	ui.Form(c, fn).Gap(10)
}

// appearancePage is what the surface looks like: its colors, its language, the
// code's font, the diff's layout and the file view.
func (sw *settingsWindow) appearancePage(c *ui.Context, pal *palette) {
	s := cfg.Get()
	section(c, msg().SettingsAppearance(), "")
	form(c, func() {
		sw.segment(c, msg().ThemeMenu(), s.Theme, []option{
			{"system", msg().ThemeSystem()},
			{"light", msg().ThemeLight()},
			{"dark", msg().ThemeDark()},
		}, func(v string) { sw.set(func(s *Settings) { s.Theme = v }) })

		// Each language named in itself, as its own readers write it, and the
		// system's for the choice that follows it.
		languages := []option{{"", msg().FollowSystem()}}
		for _, tag := range uiLanguages {
			languages = append(languages, option{tag, i18n.Autonym(tag)})
		}
		sw.selectField(c, msg().SettingsLanguage(), s.UILocale, languages,
			func(v string) { sw.set(func(s *Settings) { s.UILocale = v }) })

		ui.Field(c, msg().SettingsCodeFont(), func() {
			if ui.TextInput(c, &sw.font).Placeholder(msg().SettingsCodeFontDefault()).Changed() {
				sw.set(func(s *Settings) { s.CodeFontFamily = sw.font })
			}
		}).Description(msg().SettingsCodeFontDetail())

		ui.Field(c, msg().SettingsFontSize(), func() {
			if ui.NumberInput(c, &sw.fontSize, 10, 32, 1).Changed() {
				sw.set(func(s *Settings) { s.CodeFontSize = int(sw.fontSize) })
			}
		})

		sw.check(c, msg().WordWrap(), s.WordWrap, func(on bool) { sw.set(func(s *Settings) { s.WordWrap = on }) })
		sw.check(c, msg().ShowWhitespace(), s.ShowWhitespace, func(on bool) {
			sw.set(func(s *Settings) { s.ShowWhitespace = on })
		})

		sw.segment(c, msg().SettingsDiffLayout(), s.DiffStyle, []option{
			{"split", msg().LayoutSplit()},
			{"unified", msg().LayoutUnified()},
		}, func(v string) { sw.set(func(s *Settings) { s.DiffStyle = v }) })

		sw.segment(c, msg().SettingsFileView(), s.FileView, []option{
			{fileViewOne, msg().ViewOneFile()},
			{fileViewStack, msg().ViewStack()},
		}, func(v string) { sw.set(func(s *Settings) { s.FileView = v }) })
	})
}

// reviewPage is what an agent is asked for, and what the review shows of it.
func (sw *settingsWindow) reviewPage(c *ui.Context, pal *palette) {
	s := cfg.Get()
	section(c, msg().SettingsReview(), msg().SettingsReviewDetail())
	form(c, func() {
		sw.check(c, msg().SettingsSmartGrouping(), s.GroupingEnabled, func(on bool) {
			sw.set(func(s *Settings) { s.GroupingEnabled = on })
		})
		sw.check(c, msg().SettingsOverallSummary(), s.OverallSummary, func(on bool) {
			sw.set(func(s *Settings) { s.OverallSummary = on })
		})
		sw.check(c, msg().SettingsSummaryExpanded(), s.SummaryExpanded, func(on bool) {
			sw.set(func(s *Settings) { s.SummaryExpanded = on })
		})
		sw.check(c, msg().SettingsNotes(), s.Notes, func(on bool) {
			sw.set(func(s *Settings) { s.Notes = on })
		})

		locales := []option{{"", msg().FollowSystem()}}
		for _, tag := range summaryLocales[1:] {
			locales = append(locales, option{tag, summaryLocaleLabel(tag)})
		}
		sw.selectField(c, msg().SettingsReviewLanguage(), s.SummaryLocale, locales,
			func(v string) { sw.set(func(s *Settings) { s.SummaryLocale = v }) })

		ui.Field(c, msg().SettingsMaxFiles(), func() {
			if ui.NumberInput(c, &sw.maxFiles, 50, maxFilesLimit, 50).Changed() {
				sw.set(func(s *Settings) { s.MaxFiles = int(sw.maxFiles) })
			}
		}).Description(msg().SettingsMaxFilesDetail())

		sw.check(c, msg().SettingsAutoAnalyze(), s.AutoAnalyze, func(on bool) {
			sw.set(func(s *Settings) { s.AutoAnalyze = on })
		})
	})
}

// aiPage chooses the agent that reviews and the model it reviews with, and the
// ones a repair runs with.
func (sw *settingsWindow) aiPage(c *ui.Context, pal *palette) {
	t := c.Theme()
	s := cfg.Get()
	installed, known := installedAgents()
	section(c, msg().SettingsAI(), msg().SettingsAIDetail())
	if known && len(installed) == 0 {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			uiIcon(c, iconAlert, iconSmall).TextColor(pal.warning)
			ui.Text(c, msg().SettingsNoAgent()).FontSize(12).TextColor(t.TextMuted).Wrap().Grow(1)
		})
	}
	form(c, func() {
		ui.Field(c, msg().SettingsAgent(), func() {
			ui.Column(c).Gap(6).AlignItems(ui.Start).Children(func() {
				sw.agentSelect(c, s.AIAgent, installed, known, func(name string) {
					sw.set(func(s *Settings) {
						if s.AIAgent != name {
							s.AIAgent, s.AIModel = name, ""
						}
					})
				})
				if ui.Button(c, msg().SettingsRescan()).Clicked() {
					rescanAgents()
				}
			})
		}).Description(msg().SettingsLookedForAgents(len(installed)))

		ui.Field(c, msg().SettingsModel(), func() {
			sw.modelField(c, s.AIAgent, s.AIModel, &sw.model, func(id string) {
				sw.set(func(s *Settings) { s.AIModel = id })
			})
		})

		ui.Field(c, msg().SettingsFixAgent(), func() {
			sw.agentSelect(c, s.FixAgent, installed, known, func(name string) {
				sw.set(func(s *Settings) {
					if s.FixAgent != name {
						s.FixAgent, s.FixModel = name, ""
					}
				})
			})
		}).Description(msg().SettingsFixAgentDetail())

		ui.Field(c, msg().SettingsFixModel(), func() {
			agent := s.FixAgent
			if agent == "" {
				agent = s.AIAgent
			}
			sw.modelField(c, agent, s.FixModel, &sw.fixModel, func(id string) {
				sw.set(func(s *Settings) { s.FixModel = id })
			})
		})
	})
	ui.Text(c, msg().SettingsReadOnlyNote()).FontSize(12).TextColor(t.TextMuted).Wrap().Selectable()
}

// agentSelect chooses an agent among those the machine has, or the first it
// finds when none is chosen: the labels say the versions of the ones that are
// here.
func (sw *settingsWindow) agentSelect(c *ui.Context, current string, installed []agent.Agent, known bool, choose func(name string)) {
	options := []option{{"", msg().SettingsAutomatic()}}
	found := slices.Clone(installed)
	if a, ok := agent.ByName(current); ok && !slices.Contains(found, a) {
		// Chosen in the config file, or installed since: it still shows.
		found = append(found, a)
	}
	for _, a := range found {
		label := a.Label
		if known && !slices.Contains(installed, a) {
			label = msg().SettingsNotInstalled(a.Label)
		} else if v := versionOf(a); v != "" {
			label += " · " + v
		}
		options = append(options, option{a.Name, label})
	}
	// Until the agents are looked for, the choice shows and writes nothing
	// else: the list would change under the reader.
	sw.selectField(c, "", current, options, choose, known)
}

// modelField chooses the model an agent runs with: among those it lists, or
// typed by hand when it lists none, or none at all.
func (sw *settingsWindow) modelField(c *ui.Context, agentName, chosen string, typed *string, choose func(id string)) {
	t := c.Theme()
	a, ok := agent.ByName(agentName)
	if !ok {
		if installed, _ := installedAgents(); len(installed) > 0 {
			a, ok = installed[0], true
		}
	}
	if !ok {
		ui.Text(c, msg().SettingsNoModel()).FontSize(12).TextColor(t.TextMuted).Wrap()
		return
	}
	list := modelsOf(a, settingsModelsDir())
	if !list.done {
		ui.Text(c, msg().SettingsModelsLoading()).FontSize(12).TextColor(t.TextMuted)
		return
	}
	if len(list.models) == 0 {
		// The agent lists no model, or could not be asked: the id is typed,
		// as the config file would say it.
		if *typed != chosen {
			*typed = chosen
		}
		if ui.TextInput(c, typed).Placeholder(msg().SettingsModelPlaceholder()).Changed() {
			choose(strings.TrimSpace(*typed))
		}
		under := msg().SettingsModelTyped()
		if list.err != nil {
			under = msg().SettingsModelsFailed(shortError(list.err))
		}
		ui.Text(c, under).FontSize(11).TextColor(t.TextMuted).Wrap()
		return
	}
	options := []option{{"", msg().SettingsDefaultModel()}}
	for _, md := range list.models {
		label := md.Label
		if md.Provider != "" {
			label = md.Provider + "/" + md.Label
		}
		options = append(options, option{md.ID, label})
	}
	sw.selectField(c, "", chosen, options, choose, true)
}

// shortError is an error in the few words a field can hold.
func shortError(err error) string {
	raw := strings.TrimSpace(errorText(err))
	raw, _, _ = strings.Cut(raw, "\n")
	if len(raw) > 90 {
		raw = raw[:90] + "…"
	}
	return raw
}

// settingsModelsDir is the directory the agents are asked about: the last
// repository opened, as the config of an agent may live there, "" for none.
func settingsModelsDir() string {
	if recent := state.recent(); len(recent) > 0 {
		return recent[0]
	}
	return ""
}

// agentVersion keeps what the agents said their versions are: each is asked
// once, off the main thread.
var agentVersion struct {
	sync.Mutex
	byName map[string]string
	asked  map[string]bool
}

// versionOf is an agent's version as it last said it, "" while it is asked, or
// when it does not tell.
func versionOf(a agent.Agent) string {
	agentVersion.Lock()
	if agentVersion.byName == nil {
		agentVersion.byName, agentVersion.asked = map[string]string{}, map[string]bool{}
	}
	v, ok := agentVersion.byName[a.Name]
	ask := !ok && !agentVersion.asked[a.Name] && detectAgents
	if ask {
		agentVersion.asked[a.Name] = true
	}
	agentVersion.Unlock()
	if !ask {
		return v
	}
	go func() {
		version := agent.Version(context.Background(), a)
		agentVersion.Lock()
		agentVersion.byName[a.Name] = version
		agentVersion.Unlock()
		invalidateSettings()
	}()
	return v
}

// githubUser is the account a token is, as the settings window shows it.
type githubUser struct {
	login string
	url   string
	err   string
	asked bool
	busy  bool
}

// githubViewer asks GitHub whose a token is: it is a var, so that a test
// answers without the network.
var githubViewer = func(ctx context.Context, token string) (github.User, error) {
	return (&github.Client{Token: token}).Viewer(ctx)
}

// loadGitHub asks GitHub whose the token is, so that the page says whether it
// works: a token is not trusted until GitHub named its account. The token is
// the one in use, which is the GitHub CLI's when the settings hold none.
func (sw *settingsWindow) loadGitHub() {
	token := githubToken()
	sw.update(func() {
		sw.gh.busy = true
		sw.gh.err = ""
	})
	if token == "" {
		sw.update(func() { sw.gh = githubUser{asked: true} })
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	u, err := githubViewer(ctx, token)
	var message string
	if err != nil {
		message = shortError(err)
	}
	sw.update(func() {
		sw.gh = githubUser{login: u.Login, url: u.AvatarURL, err: message, asked: true}
		sw.invalidate()
	})
}

// loadCacheSize reads what the cache takes, off the main thread.
func (sw *settingsWindow) loadCacheSize() {
	bytes := dirSize(analysesDir()) + dirSize(runsDir())
	sw.update(func() {
		sw.cacheBytes, sw.cacheKnown = bytes, true
		sw.invalidate()
	})
}

// clearCache deletes what the app wrote and never takes back: the reviews it
// saved, and the logs of the repairs.
func (sw *settingsWindow) clearCache() {
	sw.cacheBusy = true
	sw.background(func() {
		var freed int64
		for _, dir := range []string{analysesDir(), runsDir()} {
			if dir == "" {
				continue
			}
			freed += dirSize(dir)
			os.RemoveAll(dir)
		}
		sw.update(func() {
			sw.cacheBusy, sw.cacheBytes, sw.cleared = false, 0, ""
			if freed > 0 {
				sw.cleared = byteSize(freed)
			}
		})
	})
}

// clearCacheDialog asks before the cache goes: what the app wrote is gone for
// good, so it asks twice.
func (sw *settingsWindow) clearCacheDialog(c *ui.Context) {
	if choice := ui.AlertDialog(c, &sw.confirmClear, msg().SettingsClearCacheTitle(), msg().SettingsClearCacheDetail(),
		msg().SettingsCancel(), msg().SettingsClearCacheConfirm()); choice == 1 {
		sw.clearCache()
	}
}

// dirSize is what a directory holds, in bytes.
func dirSize(dir string) int64 {
	if dir == "" {
		return 0
	}
	var total int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// byteSize is a size in the units a person reads: 0 B, 12 KB, 4.1 MB.
func byteSize(n int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	v, unit := float64(n), 0
	for v >= 1024 && unit < len(units)-1 {
		v, unit = v/1024, unit+1
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[unit])
}

// githubPage holds the token that reads and reviews pull requests, and says
// whose it is.
func (sw *settingsWindow) githubPage(c *ui.Context, pal *palette) {
	t := c.Theme()
	section(c, msg().SettingsGitHub(), msg().SettingsGitHubDetail())
	status, tone := sw.githubStatus(t, pal)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		if sw.gh.login != "" {
			avatar(c, github.User{Login: sw.gh.login, AvatarURL: sw.gh.url}, 32)
		}
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			if sw.gh.login != "" {
				ui.Text(c, sw.gh.login).FontSize(13).FontWeight(600)
			}
			ui.Text(c, status).FontSize(12).TextColor(tone).Wrap().Selectable()
		})
	})
	form(c, func() {
		ui.Field(c, msg().SettingsToken(), func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				in := ui.TextInput(c, &sw.token).Placeholder(msg().SettingsTokenPlaceholder())
				if in.Changed() {
					sw.set(func(s *Settings) { s.GithubToken = strings.TrimSpace(sw.token) })
				}
				if in.Submitted() {
					// Enter asks GitHub whose the token is now.
					sw.background(sw.loadGitHub)
				}
				if ui.Button(c, msg().SettingsClearToken()).Disabled(sw.token == "").Clicked() {
					sw.token = ""
					sw.set(func(s *Settings) { s.GithubToken = "" })
					sw.gh = githubUser{asked: true}
				}
			})
		}).Description(msg().SettingsTokenDetail())
	})
}

// githubStatus says what the token is worth: the account it is, the reason it
// was refused, or that there is none.
func (sw *settingsWindow) githubStatus(t *ui.Theme, pal *palette) (string, ui.Color) {
	switch {
	case sw.gh.busy:
		return msg().SettingsGitHubChecking(), t.TextMuted
	case sw.gh.err != "":
		return msg().SettingsGitHubBad(sw.gh.err), pal.delText
	case sw.gh.login != "":
		return msg().SettingsGitHubSignedIn(sw.gh.login), pal.viewed
	case strings.TrimSpace(cfg.Get().GithubToken) != "":
		return msg().SettingsGitHubUnchecked(), t.TextMuted
	}
	return msg().SettingsGitHubNoToken(), t.TextMuted
}

// workspacePage is where the sidebar stands, how a review opens, and what is
// remembered of it.
func (sw *settingsWindow) workspacePage(c *ui.Context, pal *palette) {
	s := cfg.Get()
	_, shown := state.layout()
	section(c, msg().SettingsWorkspace(), msg().SettingsWorkspaceDetail())
	form(c, func() {
		sw.check(c, msg().SettingsShowSidebar(), shown, func(on bool) {
			state.setSidebarShown(on)
			windowsMu.Lock()
			for _, w := range windows {
				w.sidebarShown = on
				if on && w.sidebarWidth < sidebarMin {
					w.sidebarWidth = sidebarDefault
				}
				w.rowsDirty = true
			}
			windowsMu.Unlock()
		})

		sw.segment(c, msg().SettingsSidebarSide(), s.SidebarPosition, []option{
			{"left", msg().SettingsLeft()},
			{"right", msg().SettingsRight()},
		}, func(v string) { sw.set(func(s *Settings) { s.SidebarPosition = v }) })

		sw.check(c, msg().SettingsCollapseGroups(), s.CollapseGroups, func(on bool) {
			sw.set(func(s *Settings) { s.CollapseGroups = on })
		})
		sw.check(c, msg().SettingsCollapseFiles(), s.CollapseFiles, func(on bool) {
			sw.set(func(s *Settings) { s.CollapseFiles = on })
		})
		sw.check(c, msg().SettingsRestorePosition(), s.RestorePosition, func(on bool) {
			sw.set(func(s *Settings) { s.RestorePosition = on })
		})

		ui.Field(c, msg().SettingsMaxRecent(), func() {
			if ui.NumberInput(c, &sw.maxRecent, maxRecentMin, maxRecentLimit, 1).Changed() {
				sw.set(func(s *Settings) { s.MaxRecent = int(sw.maxRecent) })
			}
		}).Description(msg().SettingsMaxRecentDetail())
	})
}

// advancedPage is the cache and the logs of the app, and what it installs.
func (sw *settingsWindow) advancedPage(c *ui.Context, pal *palette) {
	t := c.Theme()
	section(c, msg().SettingsAdvanced(), msg().SettingsAdvancedDetail())
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		usage := msg().SettingsCacheUnknown()
		switch {
		case sw.cleared != "" && sw.cacheBytes == 0:
			usage = msg().SettingsCacheCleared(sw.cleared)
		case sw.cacheKnown:
			usage = msg().SettingsCacheUsage(byteSize(sw.cacheBytes))
		}
		ui.Text(c, usage).FontSize(12).TextColor(t.TextMuted).Grow(1).Wrap().Selectable()
		if ui.Button(c, msg().SettingsClearCache()).Disabled(sw.cacheBusy).Clicked() {
			sw.confirmClear = true
		}
	})
	form(c, func() {
		ui.Field(c, msg().SettingsAnalysesKept(), func() {
			if ui.NumberInput(c, &sw.analyses, analysesKeptMin, analysesKeptLimit, 10).Changed() {
				sw.set(func(s *Settings) { s.AnalysesKept = int(sw.analyses) })
			}
		}).Description(msg().SettingsAnalysesKeptDetail())

		ui.Field(c, msg().SettingsRunsKept(), func() {
			if ui.NumberInput(c, &sw.runs, runsKeptMin, runsKeptLimit, 5).Changed() {
				sw.set(func(s *Settings) { s.RunsKept = int(sw.runs) })
			}
		}).Description(msg().SettingsRunsKeptDetail())

		ui.Field(c, msg().SettingsLogFolder(), func() {
			ui.Column(c).Gap(6).AlignItems(ui.Start).Children(func() {
				if ui.Button(c, msg().SettingsOpenLogFolder()).Clicked() {
					openPath(runsDir())
				}
				if runtime.GOOS != "windows" && ui.Button(c, msg().SettingsInstallCLI()).Clicked() {
					installCLI()
				}
			})
		}).Description(abbreviateHome(runsDir()))
	})
}

// openPath opens a folder in the file manager: it is a var, so that a test
// looks at what it was asked for without one.
var openPath = func(path string) {
	if err := mygo.Shell.OpenPath(path); err != nil {
		mygo.Dialog.Error(msg().SettingsOpenFailed(), err.Error())
	}
}

// check is a switch of one setting: its name in the column of the labels, and
// the switch beside it, which writes the setting as it flips.
func (sw *settingsWindow) check(c *ui.Context, label string, on bool, set func(on bool)) {
	sw.checkDetail(c, label, "", on, set)
}

// checkDetail is a switch with the words of what it decides under it.
func (sw *settingsWindow) checkDetail(c *ui.Context, label, detail string, on bool, set func(on bool)) {
	v := on
	f := ui.Field(c, label, func() {
		ui.Row(c).Children(func() { ui.Switch(c, &v) })
	})
	if detail != "" {
		f.Description(detail)
	}
	if v != on {
		set(v)
	}
}

// option is one choice of a drop-down or a segmented control: what it writes,
// and what it says.
type option struct{ value, label string }

// labelsOf names the choices of a drop-down.
func labelsOf(options []option) []string {
	labels := make([]string, len(options))
	for i, o := range options {
		labels[i] = o.label
	}
	return labels
}

// selectField is a drop-down of the choices given, writing the one chosen.
func (sw *settingsWindow) selectField(c *ui.Context, label, value string, options []option, set func(value string), enabled ...bool) {
	on := true
	if len(enabled) > 0 {
		on = enabled[0]
	}
	at := max(slices.IndexFunc(options, func(o option) bool { return o.value == value }), 0)
	chosen := options[at].label
	build := func() {
		sel := ui.Select(c, &chosen, labelsOf(options))
		if !on || len(options) <= 1 {
			sel.Disabled(true)
		}
	}
	if label == "" {
		build()
	} else {
		ui.Field(c, label, build)
	}
	if chosen == options[at].label {
		return
	}
	for _, o := range options {
		if o.label == chosen && o.value != value {
			set(o.value)
			return
		}
	}
}

// segment is a segmented control of the choices given, writing the one chosen.
func (sw *settingsWindow) segment(c *ui.Context, label, value string, options []option, set func(value string)) {
	at := max(slices.IndexFunc(options, func(o option) bool { return o.value == value }), 0)
	idx := at
	ui.Field(c, label, func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &idx, labelsOf(options)...)
		})
	})
	if idx != at && idx >= 0 && idx < len(options) {
		set(options[idx].value)
	}
}

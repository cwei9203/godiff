package main

import (
	"cmp"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

// The interface's language is one for the whole app, as MyGo shows one: the
// words of the catalogs in locales/ are said in it, and MyGo's own controls,
// menus and dialogs follow it too. It is read where the windows draw, and
// written there alone.

// uiLanguages are the languages the interface is written in, its own first:
// the catalogs' files, named here so that what MyGo says for itself is chosen
// among the same ones.
var uiLanguages = []string{"en", "zh-Hans"}

// interfaceLanguage is the tag the interface says its words in, of uiLanguages.
var interfaceLanguage = "en"

// followedLanguage is the language MyGo shows for a reader whose system's
// languages the settings follow, read when the app is ready: asking MyGo for it
// runs on its main loop, which a test has not, so a test sets its own.
var followedLanguage = func() string { return mygo.App.Locale() }

// applyUiLocale makes the interface say the language the settings chose, and
// MyGo's own controls with it: "" follows the reader's languages, which MyGo
// matches against uiLanguages.
func applyUiLocale(s Settings) {
	if !mygo.App.IsReady() {
		// A test: MyGo works its language out on a main loop it has not,
		// so the settings and followedLanguage are the language alone.
		interfaceLanguage = cmp.Or(s.UILocale, followedLanguage())
		return
	}
	mygo.App.SetLanguages(uiLanguages...)
	mygo.App.SetLocale(s.UILocale)
	interfaceLanguage = cmp.Or(s.UILocale, followedLanguage())
	languageChanged()
}

// languageChanged says the interface's language changed: the menu says its own
// items' labels again, and every window lays its rows out again, in the new
// words.
func languageChanged() {
	if mygo.App.IsReady() {
		mygo.App.SetMenu(buildMenu())
	}
	windowsMu.Lock()
	for _, w := range windows {
		if w.win != nil {
			w.win.Update(func() { w.rowsDirty = true })
		}
	}
	windowsMu.Unlock()
	// The welcome page is a window of its own, outside the review's list:
	// it says its words again too.
	welcomeMu.Lock()
	if welcomeWin != nil && !welcomeWin.IsDestroyed() {
		welcomeWin.Update(func() {})
	}
	welcomeMu.Unlock()
	retitleSettings()
}

// languageMenuItems choose the language the interface shows, each language
// named in itself, as its own readers write it.
func languageMenuItems(s Settings) []*mygo.MenuItem {
	items := []*mygo.MenuItem{{
		ID: "uilocale-", Label: msg().FollowSystem(), Type: mygo.MenuItemRadio, Checked: s.UILocale == "",
		Click: setting(func(s *Settings) { s.UILocale = "" }),
	}, mygo.Separator()}
	for _, tag := range uiLanguages {
		tag := tag
		items = append(items, &mygo.MenuItem{
			ID: "uilocale-" + tag, Label: i18n.Autonym(tag), Type: mygo.MenuItemRadio, Checked: s.UILocale == tag,
			Click: setting(func(s *Settings) { s.UILocale = tag }),
		})
	}
	return items
}

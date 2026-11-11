package main

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// channelDistance is how far apart two colors are, channel by channel.
func channelDistance(a, b ui.Color) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}

// TestWarningIsNotTheRedOfADeletion checks the two colors the review says
// different things with: what needs care is amber, apart from the red of what
// was deleted, what destroys and what failed, in the light and the dark.
func TestWarningIsNotTheRedOfADeletion(t *testing.T) {
	for _, theme := range []*ui.Theme{ui.LightTheme(), ui.DarkTheme()} {
		pal := paletteFor(theme)
		if pal.warning == pal.delText || pal.warning == theme.Danger {
			t.Errorf("fresh: the warning is the red of a deletion: %v", pal.warning)
		}
		if got := channelDistance(pal.warning, theme.Danger); got < 100 {
			t.Errorf("the warning %v is %d from the danger %v: too close to tell them apart", pal.warning, got, theme.Danger)
		}
		// Amber: more red than blue, in both themes.
		if pal.warning.R <= pal.warning.B {
			t.Errorf("the warning %v is not amber", pal.warning)
		}
	}
}

// TestCareIsAmber checks the band of a group that needs care: its mark is the
// warning's amber, not the red the deletions take. The badge's ground shows
// it, under the words that say so.
func TestCareIsAmber(t *testing.T) {
	w, _ := kindWindow(t)
	gi := groupIndex(t, w, "code")
	w.groups[gi].critical = true
	var pal *palette
	var theme *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) {
		theme = c.Theme()
		pal = paletteFor(theme)
		w.groupRow(c, pal, gi)
	}, 900, 200)
	tt.Frame()
	badge, ok := tt.Find("Needs care")
	if !ok {
		t.Fatalf("the band of a group that needs care says nothing of it: %q", tt.Texts())
	}
	// The ground of the badge, around the words it says.
	want := careTone(pal).Alpha(0.14).Over(pal.appBg)
	red := theme.Danger.Alpha(0.14).Over(pal.appBg)
	found := false
	for y := int(badge.Y) - 2; y < int(badge.Y+badge.H)+2 && !found; y++ {
		for x := int(badge.X) - 10; x < int(badge.X+badge.W)+10; x++ {
			got := tt.Image().RGBAAt(x, y)
			if channelDistance(ui.RGB(got.R, got.G, got.B), ui.RGB(want.R, want.G, want.B)) <= 3 {
				found = true
				break
			}
			if channelDistance(ui.RGB(got.R, got.G, got.B), ui.RGB(red.R, red.G, red.B)) <= 3 {
				t.Fatalf("the badge draws the red of a deletion %v, where it wants the warning's %v", red, want)
			}
		}
	}
	if !found {
		t.Errorf("the badge draws no ground of the warning %v", want)
	}
}

// TestFileCardTakesALineFromThePointerOrTheFileShown checks the line a file's
// card draws: none by default, and one pixel around only while the pointer is
// over the header, or on the file the surface shows.
func TestFileCardTakesALineFromThePointerOrTheFileShown(t *testing.T) {
	w, tt := kindWindow(t)
	// The tree on the left names the files too, so it goes: the header of a
	// card is the only place left saying app.go.
	w.sidebarShown = false
	w.current = fileIndex(t, w, "README.md")
	pal := paletteFor(ui.LightTheme())
	tt.Move(1, 1)
	tt.Frame()
	header, ok := tt.Find("app.go")
	if !ok {
		t.Fatalf("no header of app.go: %q", tt.Texts())
	}
	// The line a header draws shows in its first row, above its content.
	probe := func() color.RGBA {
		return tt.Image().RGBAAt(int(header.X+header.W/2), int(header.Y))
	}
	// The ground of the header, which the line replaces.
	ground := pal.surface
	lined := pal.border.Over(pal.surface)
	if got := probe(); got != (color.RGBA{R: ground.R, G: ground.G, B: ground.B, A: 255}) {
		t.Errorf("the card of a file neither shown nor under the pointer shows %v, want its ground %v", got, ground)
	}
	// The header under the pointer takes the line, and keeps it away again.
	tt.Move(header.X+header.W/2, header.Y+header.H/2)
	tt.Frame()
	if got := probe(); got != (color.RGBA{R: lined.R, G: lined.G, B: lined.B, A: 255}) {
		t.Errorf("the header under the pointer shows %v, want the line %v", got, lined)
	}
	tt.Move(1, 1)
	tt.Frame()
	if got := probe(); got != (color.RGBA{R: ground.R, G: ground.G, B: ground.B, A: 255}) {
		t.Errorf("the header kept the line after the pointer went: %v", got)
	}
	// The file the surface shows takes it too.
	w.current = fileIndex(t, w, "app.go")
	tt.Frame()
	if got := probe(); got == (color.RGBA{R: ground.R, G: ground.G, B: ground.B, A: 255}) {
		t.Errorf("the card of the file shown draws no line: %v", got)
	}
}

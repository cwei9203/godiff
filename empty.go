package main

import "github.com/egoist/mygo/ui"

// emptyBox is what a surface with nothing to show says: an icon, one line of
// what it is, one line of what to do about it, and the ways out.
//
// Every place that has nothing to show says it this way, so that the review has
// one voice for it rather than one per panel.
type emptyBox struct {
	icon   *ui.SVG
	title  string
	detail string
	// mono sets the detail in the code's font: a path, a hash, a branch, or
	// what a command said.
	mono    bool
	actions func()
	shape   emptyShape
}

// emptyShape is how much room an emptyBox takes.
type emptyShape uint8

const (
	// emptyFull is the surface itself: a panel in the middle of it.
	emptyFull emptyShape = iota
	// emptyBare is the words alone, on the surface they belong to, as the
	// welcome page has them.
	emptyBare
	// emptySmall is a corner of a panel, as the sidebar and the command bar
	// say it.
	emptySmall
	// emptyLine is one line in a bar.
	emptyLine
)

// emptyState says why a surface has nothing to show.
func emptyState(c *ui.Context, pal *palette, box emptyBox) {
	if box.shape == emptyLine {
		emptyLineOf(c, box)
		return
	}
	pad := float32(24)
	if box.shape == emptySmall {
		pad = 16
	}
	ui.Column(c).Grow(1).Center().Padding(pad).Children(func() { emptyWords(c, pal, box) })
}

// emptyWords is the icon, the one line and the way out: what an empty surface
// says, in the container that holds it.
func emptyWords(c *ui.Context, pal *palette, box emptyBox) {
	t := c.Theme()
	icon := box.icon
	if icon == nil {
		icon = iconFileDiff
	}
	size, gap, title, detail := float32(28), float32(10), float32(15), float32(13)
	if box.shape == emptySmall {
		size, gap, title, detail = 18, 6, 12, 11
	}
	panel := ui.Column(c).AlignItems(ui.Center).Gap(gap)
	if box.shape == emptyFull {
		// The panel says the review is empty; the corners of panels stand on
		// one already.
		panel.MaxWidth(520).Padding(28).Radius(radiusCard).Background(pal.surface).Border(1, pal.border)
	}
	panel.Children(func() {
		ui.Icon(c, icon).FontSize(size).TextColor(t.TextMuted)
		ui.Text(c, box.title).FontSize(title).Bold().TextAlign(ui.Center)
		if box.detail != "" {
			line := ui.Text(c, box.detail).FontSize(detail).TextColor(t.TextMuted).TextAlign(ui.Center).Selectable()
			if box.mono {
				line.Font("SF Mono, Menlo, monospace")
			}
		}
		if box.actions != nil {
			ui.Row(c).Gap(8).Margin(6, 0, 0).Children(box.actions)
		}
	})
}

// emptyLineOf says it in one line, as a bar has the room for.
func emptyLineOf(c *ui.Context, box emptyBox) {
	t := c.Theme()
	icon := box.icon
	if icon == nil {
		icon = iconFileDiff
	}
	ui.Row(c).Gap(6).Padding(2, 0).Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, box.title).FontSize(11).TextColor(t.TextMuted).Shrink(0)
		if box.detail != "" {
			ui.Text(c, box.detail).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		}
		if box.actions != nil {
			box.actions()
		}
	})
}

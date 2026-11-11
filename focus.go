package main

import "github.com/egoist/mygo/ui"

// ringWidth is the one width of the review's focus ring.
const ringWidth float32 = 2

// focusRing gives an element the review's ring around the keyboard's focus,
// and reports whether the ring shows.
//
// It is the one recipe for every place the keys go: ringWidth of palette.focus,
// on the radius of the shape itself, and only while the keys put the focus
// there. MyGo rings an element in its theme's color, so the element draws this
// ring itself and MyGo's is turned off; the ring lies just inside the element's
// edge, where no clip of a parent cuts it. A click moves the focus without
// focusVisible, and leaves the ring off, as macOS draws none.
func focusRing(e ui.Element, pal *palette, radius float32) bool {
	return focusRingWhen(e, e.FocusVisible(), pal, radius)
}

// focusRingWhen is focusRing for a ring the element shares with what holds
// the keys inside it, as a field inside the row around it.
func focusRingWhen(e ui.Element, shown bool, pal *palette, radius float32) bool {
	e.FocusRing(false)
	if shown {
		e.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(r, pal.focus, radius, ringWidth)
		})
	}
	return shown
}

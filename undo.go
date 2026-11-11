package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// undoGrace is how long the bar of an undo stays after the action it offers
// back. It is a var, so that tests need not wait for it.
var undoGrace = 5 * time.Second

// undo is a one-tap action of the review and what it took away: for a few
// seconds the bar under the surface offers to put it back, and the last action
// alone keeps its offer.
type undo struct {
	label string
	at    time.Time
	put   func()
}

// offerUndo keeps what an action took away, for the bar to offer back, in
// place of the action offered before it.
func (w *window) offerUndo(label string, put func()) {
	w.undo = &undo{label: label, at: w.now, put: put}
}

// undoBar shows the last one-tap action of the review, and the way back from
// it: a panel under the surface, over the room the sidebar's footer does not
// use, gone once undoGrace has passed.
func (w *window) undoBar(c *ui.Context, pal *palette) {
	t := c.Theme()
	u := w.undo
	if u == nil {
		return
	}
	left := undoGrace - c.Now().Sub(u.at)
	if left <= 0 {
		w.undo = nil
		return
	}
	// A frame when the bar goes, so that it goes by itself.
	c.After(left)
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(gapBlock).Justify(ui.Center).PassThrough().Children(func() {
		ui.Row(c).Height(34).Padding(0, 6, 0, 12).Gap(10).Radius(radiusCard).Background(pal.surface).
			Border(1, pal.border).Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, 0.22)).Children(func() {
			ui.Icon(c, iconCheckCircle).FontSize(13).TextColor(pal.viewed).Shrink(0)
			ui.Text(c, u.label).FontSize(12).FontWeight(600).Shrink(1).MinWidth(0)
			b := ui.ButtonBase(c).Height(22).Padding(0, 8).Radius(6).Label(textUndo()).Tooltip(msg().UndoDismiss())
			if b.Hovered() {
				b.Background(pal.hover)
			}
			b.Children(func() {
				ui.Text(c, textUndo()).FontSize(12).FontWeight(600).TextColor(t.Accent)
			})
			focusRing(b, pal, 6)
			if b.Clicked() {
				put := u.put
				w.undo = nil
				put()
			}
		})
	})
}

// dismissUndo closes the bar of an undo without undoing anything.
func (w *window) dismissUndo() { w.undo = nil }

// foldState is what a fold of the whole tree changed, to put back.
type foldState struct {
	closedDirs map[string]bool
	collapsed  []bool
}

// folded is the fold of the tree as it stands.
func (w *window) folded() foldState {
	closed := make(map[string]bool, len(w.closedDirs))
	for k, v := range w.closedDirs {
		closed[k] = v
	}
	collapsed := make([]bool, len(w.files))
	for i, f := range w.files {
		collapsed[i] = f.collapsed
	}
	return foldState{closedDirs: closed, collapsed: collapsed}
}

// refold puts the tree back the way a foldState found it.
func (w *window) refold(st foldState) {
	w.closedDirs = st.closedDirs
	for i, f := range w.files {
		if i < len(st.collapsed) {
			f.collapsed = st.collapsed[i]
		}
	}
	w.rowsDirty = true
}

// markAllViewed marks every file of the review viewed, which folds it, and
// offers the whole of it back.
func (w *window) markAllViewed() {
	before := w.folded()
	viewed := make(map[string]string, len(w.viewed))
	for k, v := range w.viewed {
		viewed[k] = v
	}
	n := 0
	for _, f := range w.files {
		if f.Fingerprint == "" || w.viewed[f.Path] == f.Fingerprint {
			continue
		}
		w.viewed[f.Path] = f.Fingerprint
		f.collapsed = true
		n++
	}
	if n == 0 {
		return
	}
	w.unviewedMiss = false
	w.rowsDirty = true
	w.writeViewed()
	w.offerUndo(msg().MarkedAllViewed(len(w.files)), func() {
		w.restoreViewed(viewed, before)
	})
}

// resetReviewState puts the review back where it began: nothing viewed, every
// group and every folder open, and every file's card open with them. The
// comments are the reader's work and not the review's state, so they stay.
func (w *window) resetReviewState() {
	before := w.folded()
	viewed := make(map[string]string, len(w.viewed))
	for k, v := range w.viewed {
		viewed[k] = v
	}
	for k := range w.viewed {
		delete(w.viewed, k)
	}
	for k := range w.closedDirs {
		delete(w.closedDirs, k)
	}
	for _, f := range w.files {
		f.collapsed = false
	}
	w.unviewedMiss = false
	w.rowsDirty = true
	w.writeViewed()
	w.offerUndo(textResetReview(), func() { w.restoreViewed(viewed, before) })
}

// restoreViewed puts the viewed marks and the folds of the tree back, and
// keeps what is remembered of them.
func (w *window) restoreViewed(viewed map[string]string, folds foldState) {
	for k := range w.viewed {
		delete(w.viewed, k)
	}
	for k, v := range viewed {
		w.viewed[k] = v
	}
	w.refold(folds)
	w.unviewedMiss = false
	w.writeViewed()
}

// writeViewed keeps what is remembered of the files viewed, all of them, as
// the store wants them by path.
func (w *window) writeViewed() {
	key := viewedKey(w.repo.Root, w.source)
	if key == "" {
		return
	}
	viewed := make(map[string]string, len(w.viewed))
	for k, v := range w.viewed {
		viewed[k] = v
	}
	paths := make([]string, 0, len(w.files))
	for _, f := range w.files {
		paths = append(paths, f.Path)
	}
	go func() {
		for _, p := range paths {
			fp := viewed[p]
			state.setViewed(key, p, fp)
		}
	}()
}

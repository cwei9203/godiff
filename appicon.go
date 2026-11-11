package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// The app's own icon, shown atop the welcome page: the one the windows and
// the Dock show, rather than a generic file.
//
//go:embed resources/icon.png
var appIconPNG []byte

// appIcon is the icon decoded once, nil when it cannot be read, and the
// welcome page falls back to the diff icon.
var appIcon = func() *ui.Bitmap {
	bitmap, err := ui.DecodeBitmap(appIconPNG)
	if err != nil {
		return nil
	}
	return bitmap
}()

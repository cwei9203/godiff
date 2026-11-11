package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// allIcons are every icon the interface can draw, by the name icons.go gives
// it. The test walks the list rather than a sample of it: an icon added to
// icons.go without a line here is a line short, which TestIconConvention
// counts.
var allIcons = []struct {
	name string
	svg  *ui.SVG
}{
	{"iconSidebar", iconSidebar},
	{"iconChevronDown", iconChevronDown},
	{"iconChevronRight", iconChevronRight},
	{"iconFolder", iconFolder},
	{"iconFile", iconFile},
	{"iconCommit", iconCommit},
	{"iconBranch", iconBranch},
	{"iconHistory", iconHistory},
	{"iconTree", iconTree},
	{"iconOpen", iconOpen},
	{"iconComment", iconComment},
	{"iconCommentPlus", iconCommentPlus},
	{"iconCopy", iconCopy},
	{"iconSplit", iconSplit},
	{"iconUnified", iconUnified},
	{"iconSearch", iconSearch},
	{"iconClose", iconClose},
	{"iconRefresh", iconRefresh},
	{"iconCheck", iconCheck},
	{"iconWrap", iconWrap},
	{"iconExpand", iconExpand},
	{"iconArrowUp", iconArrowUp},
	{"iconArrowDown", iconArrowDown},
	{"iconTrash", iconTrash},
	{"iconPencil", iconPencil},
	{"iconAlert", iconAlert},
	{"iconCommand", iconCommand},
	{"iconFileDiff", iconFileDiff},
	{"iconFolderOpen", iconFolderOpen},
	{"iconCheckCircle", iconCheckCircle},
	{"iconSparkle", iconSparkle},
	{"iconPlus", iconPlus},
	{"iconLayers", iconLayers},
	{"iconList", iconList},
	{"iconPull", iconPull},
	{"iconPullDraft", iconPullDraft},
	{"iconPullClosed", iconPullClosed},
	{"iconMerge", iconMerge},
	{"iconXCircle", iconXCircle},
	{"iconReply", iconReply},
	{"iconStop", iconStop},
	{"iconEye", iconEye},
	{"iconPalette", iconPalette},
	{"iconSliders", iconSliders},
	{"iconGear", iconGear},
}

// TestIconsParse checks every icon, one by one: it parsed at all, and it says
// the 24×24 the stroke of 2 is drawn for.
func TestIconsParse(t *testing.T) {
	for _, ic := range allIcons {
		if ic.svg == nil {
			t.Errorf("%s is nil", ic.name)
			continue
		}
		if w, h := ic.svg.Size(); w != 24 || h != 24 {
			t.Errorf("%s is %g×%g, want the 24×24 of the viewBox", ic.name, w, h)
		}
	}
}

// TestIconConvention checks the convention the icons.go header states: the
// 24×24 viewBox, no fill, the stroke of 2 in the color of the text, round caps
// and round joins. ui.SVG keeps the size of a document and not its strokes, so
// the four attributes are read where every icon takes them from, iconHead, and
// the icons that do not go through it would be icons of another set.
func TestIconConvention(t *testing.T) {
	for _, attr := range []string{
		`viewBox="0 0 24 24"`,
		`fill="none"`,
		`stroke="currentColor"`,
		`stroke-width="2"`,
		`stroke-linecap="round"`,
		`stroke-linejoin="round"`,
	} {
		if !strings.Contains(iconHead, attr) {
			t.Errorf("iconHead does not say %s: %s", attr, iconHead)
		}
	}
	source := readSource(t, "icons.go")
	if n := strings.Count(source, "= icon(`"); n != len(allIcons) {
		t.Errorf("icons.go draws %d icons, the list of the test %d: an icon is missing from allIcons", n, len(allIcons))
	}
}

// TestIconsHaveTwoSizes is the regression the batch is about: no icon is drawn
// at a size of its own. Every one goes through uiIcon at iconSmall or iconBase,
// and the two that are neither say why, in place.
func TestIconsHaveTwoSizes(t *testing.T) {
	// A call of ui.Icon with a number of its own, as ui.Icon(c, icon).FontSize(13).
	bare := regexp.MustCompile(`ui\.Icon\([^()]*\)[^\n]*\.FontSize\(\s*[0-9]`)
	// The sizes that are neither of the two: the app's icon on the page a
	// window opens on, and the icon of an empty surface. The spinner is MyGo's
	// own, at the size MyGo draws it (ui.Spinner, not ui.Icon).
	allowed := map[string]string{"view.go": "44", "empty.go": "28"}
	for file, source := range sources(t) {
		for i, line := range strings.Split(source, "\n") {
			found := bare.FindString(line)
			if found == "" {
				continue
			}
			if want, ok := allowed[file]; ok && strings.Contains(line, ".FontSize("+want+")") {
				continue
			}
			t.Errorf("%s:%d draws an icon at a size of its own: %s", file, i+1, strings.TrimSpace(line))
		}
	}
}

// sources reads the Go files of the package, the test's aside.
func sources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out[name] = readSource(t, name)
	}
	return out
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

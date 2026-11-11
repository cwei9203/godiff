// Package gc deletes what godiff writes and never takes back: the reviews it
// saved of each repository, and the logs of the repairs it ran.
//
// It deletes only files of the directories it is given, and only their
// oldest ones: a directory it cannot read, or a file it cannot touch, is
// left as it is, so that a full disk, a permission, or a race with another
// window never keeps a review from opening.
package gc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Options are the directories swept, and how much of them is kept.
type Options struct {
	// Analyses is where the reviews are saved, by source, and Runs where
	// the logs of the repairs are. An empty directory is not swept.
	Analyses string
	Runs     string
	// Slots is how many reviews a repository keeps, and Logs how many run
	// logs are kept in all: the newest, the rest go.
	Slots int
	Logs  int
	// SchemaVersion is the contract the reviews are of. One of another
	// schema never matches again: it is deleted, whatever its place.
	SchemaVersion int
}

// Report is what a sweep deleted.
type Report struct {
	Analyses int   // reviews
	Runs     int   // run logs
	Bytes    int64 // what they took
}

// Empty reports whether a sweep deleted nothing.
func (r Report) Empty() bool { return r.Analyses == 0 && r.Runs == 0 }

// Sweep deletes what Options keeps past the ones it holds, newest first, and
// logs what it deleted. It reports what it did, and never fails: what it
// cannot read or delete stays where it is.
func Sweep(o Options) Report {
	var r Report
	r.Analyses, r.Bytes = sweepReviews(o)
	n, bytes := sweepLogs(o)
	r.Runs, r.Bytes = n, r.Bytes+bytes
	if !r.Empty() {
		// godiff: cache: deleted 60 reviews and 3 run logs, 4.1 MB freed
		log.Printf("godiff: cache: deleted %d reviews and %d run logs, %s freed", r.Analyses, r.Runs, size(r.Bytes))
	}
	return r
}

// swept records the directories this process swept: a window opened on a
// repository another window opened has nothing new to sweep.
var swept struct {
	sync.Mutex
	dirs map[string]bool
}

// SweepOnce sweeps what Options names the first time this process asks for
// it, and reports whether it swept.
func SweepOnce(o Options) (Report, bool) {
	key := o.Analyses + "\x00" + o.Runs
	swept.Lock()
	sweeping := swept.dirs[key]
	if !sweeping {
		if swept.dirs == nil {
			swept.dirs = map[string]bool{}
		}
		swept.dirs[key] = true
	}
	swept.Unlock()
	if sweeping {
		return Report{}, false
	}
	return Sweep(o), true
}

// sweepReviews keeps the newest Slots reviews of each repository, and drops
// those of another schema.
func sweepReviews(o Options) (deleted int, bytes int64) {
	if o.Analyses == "" || o.Slots <= 0 {
		return 0, 0
	}
	// By the repository the review is of: they are saved side by side, one
	// per source, all of them in this one directory.
	byRoot := map[string][]entry{}
	for _, e := range newestFirst(o.Analyses) {
		if filepath.Ext(e.path) != ".json" {
			continue
		}
		root, schema, ok := readSlot(e.path)
		if !ok {
			continue
		}
		if schema != o.SchemaVersion {
			deleted, bytes = e.remove(deleted, bytes)
			continue
		}
		byRoot[root] = append(byRoot[root], e)
	}
	for _, slots := range byRoot {
		for _, e := range past(slots, o.Slots) {
			deleted, bytes = e.remove(deleted, bytes)
		}
	}
	return deleted, bytes
}

// sweepLogs keeps the newest Logs logs of the repairs.
func sweepLogs(o Options) (deleted int, bytes int64) {
	if o.Runs == "" || o.Logs <= 0 {
		return 0, 0
	}
	for _, e := range past(newestFirst(o.Runs), o.Logs) {
		deleted, bytes = e.remove(deleted, bytes)
	}
	return deleted, bytes
}

// readSlot reads what a saved review says of itself: the repository it is
// of, and the schema it was made under. A file that says neither is
// nothing to sort, and stays.
func readSlot(path string) (root string, schema int, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("godiff: cache: %v", err)
		return "", 0, false
	}
	var saved struct {
		Root     string `json:"root"`
		Analysis *struct {
			SchemaVersion int `json:"schemaVersion"`
		} `json:"analysis"`
	}
	if json.Unmarshal(data, &saved) != nil || saved.Analysis == nil {
		return "", 0, false
	}
	return saved.Root, saved.Analysis.SchemaVersion, true
}

// entry is a file a sweep may delete, with what deleting it takes.
type entry struct {
	path  string
	when  time.Time
	bytes int64
}

// remove deletes an entry, and counts it when it did: one already gone, or
// one the sweep may not touch, is nothing to count.
func (e entry) remove(deleted int, bytes int64) (int, int64) {
	if err := os.Remove(e.path); err != nil {
		log.Printf("godiff: cache: %v", err)
		return deleted, bytes
	}
	return deleted + 1, bytes + e.bytes
}

// newestFirst lists the files of a directory, the newest first, which are
// the ones kept. A directory that is not there holds nothing, and one that
// cannot be read says so.
func newestFirst(dir string) []entry {
	items, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("godiff: cache: %v", err)
		}
		return nil
	}
	out := make([]entry, 0, len(items))
	for _, it := range items {
		// The sweep deletes only what godiff writes: no directory, and no
		// file it hides.
		if it.IsDir() || strings.HasPrefix(it.Name(), ".") {
			continue
		}
		info, err := it.Info()
		if err != nil {
			log.Printf("godiff: cache: %v", err)
			continue
		}
		out = append(out, entry{path: filepath.Join(dir, it.Name()), when: info.ModTime(), bytes: info.Size()})
	}
	slices.SortStableFunc(out, func(a, b entry) int {
		return b.when.Compare(a.when)
	})
	return out
}

// past is the entries after the first keep of them, which are the ones a
// sweep deletes.
func past(entries []entry, keep int) []entry {
	if len(entries) <= keep {
		return nil
	}
	return entries[keep:]
}

// size writes a count of bytes as a reader counts them: 812 B, 4.1 kB.
func size(b int64) string {
	switch {
	case b < 1_000:
		return fmt.Sprintf("%d B", b)
	case b < 1_000_000:
		return fmt.Sprintf("%.1f kB", float64(b)/1_000)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/1_000_000)
	}
}

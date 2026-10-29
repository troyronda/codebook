// Package sample picks a spread of Markdown files from a folder for an eval.
package sample

import (
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Options configures a sample.
type Options struct {
	// N is how many files to pick.
	N int
	// Seed makes the sample repeatable.
	Seed uint64
	// Skip names folders to leave out wherever they appear, such as "attachments".
	// "." leaves out the files directly in root (a vault's README and the like).
	// Hidden folders and files are always left out.
	Skip []string
	// Settle leaves out files changed within this long of Now, which may still be
	// syncing (default 60 seconds).
	Settle time.Duration
	// Now is the current time (default time.Now).
	Now time.Time
}

// Skipped counts files left out, by reason.
type Skipped struct {
	Conflict int // an iCloud conflict copy, "<name> 2.md" beside "<name>.md"
	Recent   int // changed within Settle
}

// conflictRE matches the copy iCloud makes when two devices change a file at once.
var conflictRE = regexp.MustCompile(`^(.+) [0-9]+\.md$`)

// Files returns up to o.N Markdown files under root, as slash-separated paths relative
// to root, spread evenly across its top-level folders (files at the top level count as
// one folder, "."): one from each folder in turn until N are picked or none are left.
func Files(root string, o Options) ([]string, Skipped, error) {
	if o.Settle == 0 {
		o.Settle = 60 * time.Second
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	var sk Skipped
	groups := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != root && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && slices.Contains(o.Skip, name) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		if m := conflictRE.FindStringSubmatch(name); m != nil {
			if _, err := os.Stat(filepath.Join(filepath.Dir(p), m[1]+".md")); err == nil {
				sk.Conflict++
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if o.Now.Sub(info.ModTime()) < o.Settle {
			sk.Recent++
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		top := "."
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			top = rel[:i]
		} else if slices.Contains(o.Skip, ".") {
			return nil
		}
		groups[top] = append(groups[top], rel)
		return nil
	})
	if err != nil {
		return nil, sk, err
	}

	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	slices.Sort(names)
	// Shuffle each group in name order so the same seed always gives the same sample.
	rng := rand.New(rand.NewPCG(o.Seed, o.Seed))
	for _, g := range names {
		files := groups[g]
		slices.Sort(files)
		rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	}
	var out []string
	for len(out) < o.N {
		picked := false
		for _, g := range names {
			if len(out) == o.N {
				break
			}
			if len(groups[g]) > 0 {
				out = append(out, groups[g][0])
				groups[g] = groups[g][1:]
				picked = true
			}
		}
		if !picked {
			break
		}
	}
	return out, sk, nil
}

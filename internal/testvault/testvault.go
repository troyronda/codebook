// Package testvault copies a fixture folder for tests, with file times set in the past,
// since a checkout gives every file the current time.
package testvault

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Copy copies src to a temporary folder and returns its path. Every file's time is set
// to an hour before now.
func Copy(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	old := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		return os.Chtimes(target, old, old)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

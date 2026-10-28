package sample_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/troyronda/codebook/internal/sample"
	"github.com/troyronda/codebook/internal/testvault"
)

func TestFiles(t *testing.T) {
	root := testvault.Copy(t, "../../testdata/notes")
	// A note still being written.
	if err := os.WriteFile(filepath.Join(root, "journal", "today.md"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	all, sk, err := sample.Files(root, sample.Options{N: 100, Skip: []string{"attachments"}})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(all)
	want := []string{"chats/squash.md", "inbox-note.md", "journal/beans.md", "journal/compost.md", "journal/walk.md", "reading/books/soil-book.md", "reading/tools-article.md"}
	if !slices.Equal(all, want) {
		t.Errorf("files %v, want %v", all, want)
	}
	if sk.Conflict != 1 || sk.Recent != 1 {
		t.Errorf("skipped %+v, want one conflict copy and one recent file", sk)
	}

	// Four picks from four folders (".", chats, journal, reading): one from each.
	four, _, err := sample.Files(root, sample.Options{N: 4, Seed: 7, Skip: []string{"attachments"}})
	if err != nil {
		t.Fatal(err)
	}
	tops := map[string]bool{}
	for _, f := range four {
		top, _, ok := strings.Cut(f, "/")
		if !ok {
			top = "."
		}
		tops[top] = true
	}
	if len(four) != 4 || len(tops) != 4 {
		t.Errorf("sample of 4 %v isn't spread across the 4 folders", four)
	}
	again, _, _ := sample.Files(root, sample.Options{N: 4, Seed: 7, Skip: []string{"attachments"}})
	if !slices.Equal(four, again) {
		t.Errorf("same seed gave %v then %v", four, again)
	}
}

func TestConflictNameWithoutOriginal(t *testing.T) {
	root := t.TempDir()
	// "Chapter 2.md" with no "Chapter.md" beside it is an ordinary note.
	p := filepath.Join(root, "Chapter 2.md")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := anHourAgo()
	_ = os.Chtimes(p, old, old)
	files, sk, err := sample.Files(root, sample.Options{N: 5})
	if err != nil || len(files) != 1 || sk.Conflict != 0 {
		t.Fatalf("files %v skipped %+v err %v", files, sk, err)
	}
}

func anHourAgo() time.Time { return time.Now().Add(-time.Hour) }

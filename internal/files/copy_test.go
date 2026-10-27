package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyTreeRejectsOverlappingDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	put(t, filepath.Join(source, "keep.txt"), "original")
	for _, destination := range []string{source, filepath.Join(source, "nested", "copy"), root} {
		if err := CopyTree(source, destination); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("overlapping copy %s -> %s: %v", source, destination, err)
		}
	}
	if read(t, filepath.Join(source, "keep.txt")) != "original" {
		t.Fatal("refused copy modified source")
	}
	if Exists(filepath.Join(source, "nested")) {
		t.Fatal("refused copy created destination")
	}
	sibling := filepath.Join(root, "source-sibling")
	if err := CopyTree(source, sibling); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(sibling, "keep.txt")) != "original" {
		t.Fatal("sibling copy failed")
	}
}

func TestCanonicalMissingDescendants(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(filepath.Join(root, "not-created", "child"))
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(resolved, "not-created", "child")
	if got != expected {
		t.Fatalf("got %q, want %q", got, expected)
	}
	if _, err = os.Stat(filepath.Join(root, "not-created")); !os.IsNotExist(err) {
		t.Fatal("canonicalization created a destination")
	}
}

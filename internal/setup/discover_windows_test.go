package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryHandlesNestedExecutable(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "engine/bin/tool.exe")
	put(t, entry, data)
	profile := Profile{ID: "custom"}
	profile.Adapter.Executable = "bin/tool.exe"
	profile.Instance.Runtime = "engine"
	for _, selection := range []string{root, filepath.Join(root, "engine"), entry} {
		found, err := Discover(selection, []Profile{profile})
		if err != nil || len(found) != 1 || found[0].Root != root || found[0].Runtime != "engine" {
			t.Fatalf("%s: %+v, %v", selection, found, err)
		}
	}
}

func TestDiscoveryDoesNotSearchRecursively(t *testing.T) {
	root, _ := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "deep/nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "duckstation"), filepath.Join(root, "deep/nested/engine")); err != nil {
		t.Fatal(err)
	}
	profiles, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	found, err := Discover(root, profiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatal("unexpected recursive search", found)
	}
}

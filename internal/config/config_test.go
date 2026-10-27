package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestValidation(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "util/portable-update/update.json")
	put(t, p, `{"schema":2,"name":"test","adapter":"../outside.json","runtime":"pcsx2","channel":"nightly","saves":[]}`)
	if _, e := Load(p); e == nil || !strings.Contains(e.Error(), "unsafe") {
		t.Fatalf("escaping adapter accepted: %v", e)
	}
}
func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0644); e != nil {
		t.Fatal(e)
	}
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

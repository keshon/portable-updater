package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingDefaultConfigStartsSetup(t *testing.T) {
	for _, location := range []string{"portable-update.exe", "util/portable-update/portable-update.exe"} {
		t.Run(location, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), location)
			var out, stderr bytes.Buffer
			err := run(nil, &out, &stderr, executable, strings.NewReader(""))
			if err != nil || !strings.Contains(out.String(), "first-time setup") {
				t.Fatalf("expected setup, got %v; %s", err, out.String())
			}
		})
	}
}

func TestExplicitOperationsDoNotFallBackToSetup(t *testing.T) {
	for _, mode := range []string{"rollback", "explicit-manifest", "invalid-config"} {
		t.Run(mode, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "util/portable-update")
			if err := os.MkdirAll(home, 0755); err != nil {
				t.Fatal(err)
			}
			var args []string
			switch mode {
			case "rollback":
				args = []string{"--rollback"}
			case "explicit-manifest":
				args = []string{"--manifest", filepath.Join(home, "missing.json")}
			case "invalid-config":
				if err := os.WriteFile(filepath.Join(home, "update.json"), []byte("invalid"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var out, stderr bytes.Buffer
			if err := run(args, &out, &stderr, filepath.Join(home, "portable-update.exe"), strings.NewReader("")); err == nil {
				t.Fatal("expected configuration error")
			}
			if strings.Contains(out.String(), "first-time setup") {
				t.Fatal("unexpected setup fallback")
			}
		})
	}
}

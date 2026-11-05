package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableUnpackPolicy(t *testing.T) {
	for _, mode := range []string{"valid", "valid-numeric", "unknown-format", "optional-checksum", "missing-destination", "duplicate-destination", "unknown-placeholder", "empty-arguments", "archive-with-arguments", "unpack-with-companions", "entrypoint-preservation", "ancestor-preservation"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "util/portable-update")
			data, err := os.ReadFile("../../adapters/koboldcpp-windows.json")
			if err != nil {
				t.Fatal(err)
			}
			var adapter Adapter
			if err := json.Unmarshal(data, &adapter); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "unpack-with-companions":
				adapter.Source.CompanionPatterns = []string{`^deps.zip$`}
			case "entrypoint-preservation":
				adapter.Preserve = []string{adapter.Executable}
			case "ancestor-preservation":
				adapter.Executable = "bin/app.exe"
				adapter.Preserve = []string{"bin"}
			case "valid-numeric":
				adapter.Version.Kind = "windows-file-version"
			case "duplicate-destination":
				adapter.Unpack.Arguments = []string{"{destination}", "{destination}"}
			case "unknown-placeholder":
				adapter.Unpack.Arguments = []string{"{destination}", "{source}"}
			case "empty-arguments":
				adapter.Unpack.Arguments = nil
			case "archive-with-arguments":
				adapter.Package = "archive"
			case "unknown-format":
				adapter.Package = "shell-command"
			case "optional-checksum":
				adapter.Source.Checksum = "when-published"
			case "missing-destination":
				adapter.Unpack.Arguments = []string{"--unpack"}
			}
			data, err = json.Marshal(adapter)
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(home, "adapters/koboldcpp-windows.json"), string(data))
			example, err := os.ReadFile("../../examples/koboldcpp.json")
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(home, "update.json"), string(example))
			_, err = Load(filepath.Join(home, "update.json"))
			if strings.HasPrefix(mode, "valid") && err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(mode, "valid") && err == nil {
				t.Fatal("unsafe package policy accepted")
			}
		})
	}
}

func TestManifestValidation(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "util/portable-update/update.json")
	put(t, p, `{"schema":2,"name":"test","adapter":"../outside.json","runtime":"pcsx2","channel":"nightly","saves":[]}`)
	if _, e := Load(p); e == nil || !strings.Contains(e.Error(), "unsafe") {
		t.Fatalf("escaping adapter accepted: %v", e)
	}
}

func TestDecodeFileRejectsTrailingData(t *testing.T) {
	for _, suffix := range []string{"]", "}", "{}", "garbage"} {
		t.Run(suffix, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "input.json")
			put(t, p, `{"schema":2}`+suffix)
			var value Instance
			if err := decodeFile(p, &value); err == nil {
				t.Fatal("trailing JSON accepted")
			}
		})
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

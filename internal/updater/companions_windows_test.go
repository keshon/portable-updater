package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/release"
)

func archiveBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var body bytes.Buffer
	z := zip.NewWriter(&body)
	for name, data := range entries {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return body.Bytes()
}

func TestCompanionArchivesAreOneTransaction(t *testing.T) {
	for _, mode := range []string{"success", "collision", "bad-digest", "missing-file", "missing-selection"} {
		t.Run(mode, func(t *testing.T) {
			u, _ := fixture(t)
			u.logWriter = io.Discard
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			binary, e := os.ReadFile(exe)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(u.runtime(), "emu.exe"), binary, 0644); e != nil {
				t.Fatal(e)
			}
			put(t, filepath.Join(u.runtime(), "dependency.dll"), "original dependency")
			primary := archiveBytes(t, map[string][]byte{"emu.exe": binary})
			entries := map[string][]byte{"dependency.dll": []byte("first dependency")}
			if mode == "collision" {
				entries["emu.exe"] = binary
			}
			if mode == "missing-file" {
				entries = map[string][]byte{"other.dll": []byte("other")}
			}
			first := archiveBytes(t, entries)
			second := archiveBytes(t, map[string][]byte{"dependency.dll": []byte("second dependency")})
			bodies := map[string][]byte{"/app.zip": primary, "/deps-v1.zip": first, "/deps-v2.zip": second}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(bodies[r.URL.Path]))
			}))
			defer server.Close()
			asset := func(path, name string) release.Asset {
				data := bodies[path]
				digest := sha256.Sum256(data)
				return release.Asset{Name: name, URL: server.URL + path, Size: int64(len(data)), Digest: fmt.Sprintf("sha256:%x", digest)}
			}
			provider := &fixedProvider{selection: release.Selection{Tag: "v1", Asset: asset("/app.zip", "app.zip"), Companions: []release.Asset{asset("/deps-v1.zip", "deps.zip")}}}
			if mode == "bad-digest" {
				provider.selection.Companions[0].Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
			if mode == "missing-selection" {
				provider.selection.Companions = nil
			}
			adapter := config.Adapter{ID: "fixture", Executable: "emu.exe", Source: release.Source{CompanionPatterns: []string{`^deps.zip$`}}, RequiredPackage: []string{"emu.exe", "dependency.dll"}}
			adapter.Version.Kind = "artifact-sha256"
			u.policy.RequiredPackage = adapter.RequiredPackage
			project := Engine{transaction: u, provider: provider, Adapter: adapter, Instance: config.Instance{Channel: "nightly"}}
			err := project.Update(context.Background(), io.Discard)
			if mode != "success" {
				if err == nil {
					t.Fatal("invalid package set accepted")
				}
				if read(t, filepath.Join(u.runtime(), "dependency.dll")) != "original dependency" {
					t.Fatal("failed set changed installed runtime")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if read(t, filepath.Join(u.runtime(), "dependency.dll")) != "first dependency" {
				t.Fatal("companion not installed")
			}
			phase := u.state
			if e = project.Update(context.Background(), io.Discard); e != nil {
				t.Fatal(e)
			}
			if u.state != phase {
				t.Fatal("current release was replaced again")
			}
			// A changed companion must invalidate a receipt even when the main asset and tag are identical.
			provider.selection.Companions = []release.Asset{asset("/deps-v2.zip", "deps.zip")}
			if e = project.Update(context.Background(), io.Discard); e != nil {
				t.Fatal(e)
			}
			if read(t, filepath.Join(u.runtime(), "dependency.dll")) != "second dependency" {
				t.Fatal("changed companion was missed")
			}
			if e = project.Restore(io.Discard); e != nil {
				t.Fatal(e)
			}
			if read(t, filepath.Join(u.runtime(), "dependency.dll")) != "first dependency" {
				t.Fatal("rollback did not restore whole package set")
			}
			if read(t, filepath.Join(u.root, "data/saves/progress")) != "save" {
				t.Fatal("external data changed")
			}
		})
	}
}

func TestCompanionHonorsRemainingExtractionBudget(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "deps.zip")
	if e := os.WriteFile(archive, archiveBytes(t, map[string][]byte{"dependency.dll": []byte("too large")}), 0644); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(root, "candidate")
	if e := os.Mkdir(dest, 0755); e != nil {
		t.Fatal(e)
	}
	if e := files.ExtractLimit(archive, dest, 1); e == nil {
		t.Fatal("budget bypassed")
	}
	if files.Exists(filepath.Join(dest, "dependency.dll")) {
		t.Fatal("wrote file before checking budget")
	}
}

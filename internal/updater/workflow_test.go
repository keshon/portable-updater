package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/release"
)

func TestFailedUpdateRetainsEarlierRollback(t *testing.T) {
	for _, failure := range []string{"validation", "preservation", "swap"} {
		t.Run(failure, func(t *testing.T) {
			u, source := fixture(t)
			u.logWriter = io.Discard
			if err := u.stage(source); err != nil {
				t.Fatal(err)
			}
			if err := u.apply(false); err != nil {
				t.Fatal(err)
			}
			prior := u.state
			var body bytes.Buffer
			z := zip.NewWriter(&body)
			w, err := z.Create("emu.exe")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write([]byte("third version")); err != nil {
				t.Fatal(err)
			}
			if failure == "preservation" {
				w, err = z.Create("config")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Write([]byte("upstream file conflicts with settings directory")); err != nil {
					t.Fatal(err)
				}
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprint(body.Len()))
				w.Write(body.Bytes())
			}))
			defer server.Close()
			digest := sha256.Sum256(body.Bytes())
			provider := &fixedProvider{selection: release.Selection{Tag: "v3.0.0", Asset: release.Asset{Name: "update.zip", URL: server.URL + "/update.zip", Size: int64(body.Len()), Digest: fmt.Sprintf("sha256:%x", digest)}}}
			adapter := config.Adapter{RequiredPackage: []string{"emu.exe"}}
			adapter.Version.Kind = "windows-file-version"
			if failure == "validation" {
				adapter.RequiredPackage = append(adapter.RequiredPackage, "missing.dll")
			}
			if failure == "preservation" {
				u.policy.Preserve = []string{"config/settings.ini"}
			}
			originalVersion := executableVersion
			t.Cleanup(func() { executableVersion = originalVersion })
			executableVersion = func(path string) (string, error) {
				if files.Within(u.runtime(), path) {
					return "2.0.0", nil
				}
				return "3.0.0", nil
			}
			originalRename := renamePath
			t.Cleanup(func() { renamePath = originalRename })
			if failure == "swap" {
				renamePath = func(from, to string) error {
					if to == u.runtime() && strings.Contains(filepath.Base(from), ".next-") {
						return fmt.Errorf("injected swap failure")
					}
					return originalRename(from, to)
				}
			}
			project := Engine{transaction: u, provider: provider, Adapter: adapter, Instance: config.Instance{Channel: "stable"}}
			if err := project.Update(context.Background(), io.Discard); err == nil {
				t.Fatal("failure was ignored")
			}
			if u.state != prior {
				t.Fatalf("prior rollback journal consumed: %+v", u.state)
			}
			b, err := os.ReadFile(u.journalPath())
			if err != nil {
				t.Fatal(err)
			}
			var disk journal
			if err = json.Unmarshal(b, &disk); err != nil || disk != prior {
				t.Fatal("prior journal not retained on disk")
			}
			if err := u.rollback(); err != nil {
				t.Fatal(err)
			}
			if read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
				t.Fatal("earlier runtime was not restored")
			}
		})
	}
}

type fixedProvider struct {
	selection release.Selection
	calls     int
}

func (p *fixedProvider) Latest(context.Context, release.Source, string) (release.Selection, error) {
	p.calls++
	return p.selection, nil
}
func TestAlreadyCurrentDoesNotDownloadOrSwap(t *testing.T) {
	u, _ := fixture(t)
	provider := &fixedProvider{selection: release.Selection{Tag: "v2.9.146"}}
	originalVersion := executableVersion
	executableVersion = func(string) (string, error) { return "2.9.146.0", nil }
	t.Cleanup(func() { executableVersion = originalVersion })
	project := Engine{provider: provider, transaction: u, Instance: config.Instance{Channel: "nightly"}, Adapter: config.Adapter{Source: release.Source{Provider: "fixture"}}}
	if e := project.Update(context.Background(), io.Discard); e != nil {
		t.Fatal(e)
	}
	if provider.calls != 1 || u.state.Phase != "" {
		t.Fatal("already-current case mutated update state")
	}
	if files.Exists(filepath.Join(u.work, "cache")) {
		t.Fatal("already-current case downloaded")
	}
}

func TestProviderCannotBypassRequiredChecksumPolicy(t *testing.T) {
	for _, policy := range []string{"", "required"} {
		t.Run("policy-"+policy, func(t *testing.T) {
			u, _ := fixture(t)
			original := executableVersion
			t.Cleanup(func() { executableVersion = original })
			executableVersion = func(string) (string, error) { return "1.0.0", nil }
			provider := &fixedProvider{selection: release.Selection{Tag: "v2.0.0", Asset: release.Asset{Name: "package.zip", URL: "https://example.invalid/package.zip", Size: 10}}}
			adapter := config.Adapter{Source: release.Source{Checksum: policy}}
			project := Engine{transaction: u, provider: provider, Adapter: adapter, Instance: config.Instance{Channel: "stable"}}
			if err := project.Update(context.Background(), io.Discard); err == nil || !strings.Contains(err.Error(), "checksum policy") {
				t.Fatal("provider bypassed integrity policy:", err)
			}
			if files.Exists(filepath.Join(u.work, "cache")) {
				t.Fatal("unverified package download started")
			}
		})
	}
}
func TestCandidateWrongVersionRefused(t *testing.T) {
	u, _ := fixture(t)
	original := executableVersion
	executableVersion = func(string) (string, error) { return "2.8.1.0", nil }
	t.Cleanup(func() { executableVersion = original })
	project := Engine{transaction: u, Adapter: config.Adapter{RequiredPackage: []string{"emu.exe"}}}
	expected, _ := parseVersion("2.9.146")
	if e := project.validateCandidate(u.runtime(), expected); e == nil {
		t.Fatal("wrong executable version accepted")
	}
}

func TestAdapterAndInstanceFilesLoad(t *testing.T) {
	root := t.TempDir()
	data, e := os.ReadFile("../../adapters/pcsx2-windows.json")
	if e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(root, "util/portable-update/adapters/pcsx2-windows.json"), string(data))
	put(t, filepath.Join(root, "util/portable-update/update.json"), `{"schema":2,"name":"test","adapter":"adapters/pcsx2-windows.json","runtime":"pcsx2","channel":"nightly","saves":["data/memcards"]}`)
	c, e := config.Load(filepath.Join(root, "util/portable-update/update.json"))
	if e != nil {
		t.Fatal(e)
	}
	p, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	if p.policy.Executable != "pcsx2-qt.exe" || len(p.policy.Preserve) != 4 {
		t.Fatal("adapter was not resolved")
	}
	put(t, p.journalPath(), `{"phase":"applied","previous":"game","stage":"pcsx2.next-test"}`)
	if _, e := New(c); e == nil || !strings.Contains(e.Error(), "previous runtime") {
		t.Fatalf("foreign rollback folder accepted: %v", e)
	}
}

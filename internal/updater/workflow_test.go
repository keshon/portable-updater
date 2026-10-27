package updater

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/release"
)

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

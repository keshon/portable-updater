package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bundle "github.com/keshon/portable-updater"
	"github.com/keshon/portable-updater/internal/files"
	"github.com/keshon/portable-updater/internal/httpx"
	"github.com/keshon/portable-updater/internal/platform"
)

func put(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVersionComparison(t *testing.T) {
	for _, tt := range []struct {
		latest, current string
		want            bool
	}{
		{"v1.10.0", "v1.9.9", true}, {"v1.0.0", "v2.0.0", false},
		{"v1.0.0", "v1.0.0", false}, {"v0.1.0", "dev-b21c16f", true},
		{"v1.0.0-beta", "dev", false}, {"v1.2.100000000000000000000", "v1.2.9", true},
	} {
		if got := newer(tt.latest, tt.current); got != tt.want {
			t.Errorf("%s > %s: %v", tt.latest, tt.current, got)
		}
	}
}

func TestPlanPreservesCustomDefinitionsConfigurationAndState(t *testing.T) {
	root := t.TempDir()
	home, stage := filepath.Join(root, "util/portable-update"), filepath.Join(root, "package")
	put(t, home, "portable-update.exe", []byte("old binary"))
	put(t, stage, "portable-update.exe", []byte("new binary"))
	canonical, err := bundle.Files.ReadFile("schemas/adapter.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	put(t, home, "schemas/adapter.schema.json", canonical)
	put(t, stage, "schemas/adapter.schema.json", []byte("new schema"))
	put(t, home, "adapters/pcsx2-windows.json", []byte("custom definition"))
	put(t, stage, "adapters/pcsx2-windows.json", []byte("upstream change"))
	put(t, home, "update.json", []byte("instance"))
	put(t, home, "state/update-state.json", []byte("journal"))
	p, skipped, err := prepare(home, stage, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 || len(skipped) != 1 || skipped[0] != "adapters/pcsx2-windows.json" {
		t.Fatalf("unexpected plan: %+v; skipped %v", p, skipped)
	}
	// A schema and binary update leave the custom adapter, instance and journal
	// alone. Missing optional bundled files are not silently introduced.
	stageRoot := filepath.Dir(stage)
	// applyFiles expects a package subdirectory at stageRoot.
	if err = applyFiles(home, stageRoot, p, io.Discard); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"portable-update.exe": "new binary", "schemas/adapter.schema.json": "new schema", "adapters/pcsx2-windows.json": "custom definition", "update.json": "instance", "state/update-state.json": "journal"} {
		data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(name)))
		if err != nil || string(data) != want {
			t.Fatalf("%s changed incorrectly: %s, %v", name, data, err)
		}
	}
}

func TestApplyChecksAllHashesAndRollsBackReturnedFailure(t *testing.T) {
	for _, mode := range []string{"tampered", "backup-conflict", "forbidden-target", "linked-target"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			home, stage := filepath.Join(root, "home"), filepath.Join(root, "stage")
			var p plan
			for _, name := range []string{"LICENSE", "portable-update.exe"} {
				a := put(t, home, name, []byte("old "+name))
				b := put(t, stage, "package/"+name, []byte("new "+name))
				before, _ := files.Digest(a)
				after, _ := files.Digest(b)
				p.Files = append(p.Files, replacement{name, before, after})
			}
			switch mode {
			case "tampered":
				put(t, stage, "package/portable-update.exe", []byte("tampered"))
			case "backup-conflict":
				put(t, stage, "previous/portable-update.exe", []byte("existing backup"))
			case "forbidden-target":
				p.Files[1].Path = "update.json"
			case "linked-target":
				path := filepath.Join(home, "LICENSE")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				external := put(t, root, "outside", []byte("old LICENSE"))
				if err := os.Symlink(external, path); err != nil {
					t.Skip("symlink creation unavailable:", err)
				}
			}
			if err := applyFiles(home, stage, p, io.Discard); err == nil {
				t.Fatal("unsafe or failed update accepted")
			}
			for _, name := range []string{"LICENSE", "portable-update.exe"} {
				data, err := os.ReadFile(filepath.Join(home, name))
				if err != nil || string(data) != "old "+name {
					t.Fatalf("old file not retained: %s, %v", data, err)
				}
			}
		})
	}
}

// The test executable plays both parent and helper, exercising actual Windows
// image locks rather than pretending an ordinary data file is a running updater.
func TestSelfUpdateProcess(t *testing.T) {
	mode := os.Getenv("PORTABLE_SELFUPDATE_TEST")
	if mode == "helper" {
		if err := Apply(os.Getenv("PORTABLE_SELFUPDATE_PLAN"), os.Stdout); err != nil {
			t.Fatal(err)
		}
		return
	}
	if mode == "parent" {
		executable, _ := os.Executable()
		unlock, err := platform.LockUpdater(filepath.Join(filepath.Dir(executable), "updater.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		stage := filepath.Dir(os.Getenv("PORTABLE_SELFUPDATE_PLAN"))
		cmd := exec.Command(filepath.Join(stage, "helper.exe"), "-test.run=^TestSelfUpdateProcess$")
		cmd.Env = append(os.Environ(), "PORTABLE_SELFUPDATE_TEST=helper")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Process.Release(); err != nil {
			t.Fatal(err)
		}
		// Hold the real executable image and updater lock after helper startup.
		time.Sleep(300 * time.Millisecond)
		return
	}
	root := t.TempDir()
	home, stage := filepath.Join(root, "util/portable-update"), filepath.Join(root, "tmp/portable-update/self-update/test")
	testBinary, _ := os.Executable()
	put(t, home, "placeholder", nil)
	put(t, stage, "placeholder", nil)
	target := filepath.Join(home, "portable-update.exe")
	if err := files.CopyFile(testBinary, target); err != nil {
		t.Fatal(err)
	}
	if err := files.CopyFile(testBinary, filepath.Join(stage, "helper.exe")); err != nil {
		t.Fatal(err)
	}
	put(t, stage, "package/portable-update.exe", []byte("verified replacement fixture"))
	before, _ := files.Digest(target)
	after, _ := files.Digest(filepath.Join(stage, "package/portable-update.exe"))
	p := plan{Version: "v1.0.0", Files: []replacement{{"portable-update.exe", before, after}}}
	data, _ := json.Marshal(p)
	planPath := put(t, stage, "plan.json", data)
	cmd := exec.Command(target, "-test.run=^TestSelfUpdateProcess$")
	cmd.Env = append(os.Environ(), "PORTABLE_SELFUPDATE_TEST=parent", "PORTABLE_SELFUPDATE_PLAN="+planPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("parent: %s %v", output, err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		log, _ := os.ReadFile(filepath.Join(stage, "result.log"))
		if bytes.Contains(log, []byte("updated to v1.0.0")) {
			break
		}
		if bytes.Contains(log, []byte("failed")) || time.Now().After(deadline) {
			t.Fatalf("helper did not finish: %s", log)
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "verified replacement fixture" {
		t.Fatalf("wrong installed binary: %s", got)
	}
	backup, _ := files.Digest(filepath.Join(stage, "previous/portable-update.exe"))
	if backup != before {
		t.Fatal("old executable backup missing")
	}
	// Allow the helper to exit before TempDir cleanup tries deleting its image.
	for time.Now().Before(deadline) {
		if err := os.Rename(filepath.Join(stage, "helper.exe"), filepath.Join(stage, "helper-finished.exe")); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestHomeRejectsStandaloneAndLinkedLocations(t *testing.T) {
	if _, _, err := homeFor(filepath.Join(t.TempDir(), "portable-update.exe")); err == nil || !strings.Contains(err.Error(), "setup first") {
		t.Fatal("standalone accepted", err)
	}
}

func TestVerifiedReleaseThroughHelperHandoff(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "util/portable-update")
	put(t, home, "placeholder", nil)
	candidate := filepath.Join(root, "release.exe")
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w -X=github.com/keshon/portable-updater/internal/app.Version=v0.2.0", "-o", candidate, "../../cmd/portable-updater")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %s: %v", output, err)
	}
	target := filepath.Join(home, "portable-update.exe")
	if err := files.CopyFile(candidate, target); err != nil {
		t.Fatal(err)
	}
	old, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.WriteString("old distribution overlay"); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	// No configuration is required or generated by self-update.
	put(t, home, "update.json", []byte("operator configuration; not parsed or changed"))
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	data, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := zw.Create("portable-update.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(archive.Bytes()))
	// All production HTTPS requests are redirected to this isolated fixture.
	// There is no dependency on an existing public release or the network.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/keshon/portable-updater/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","assets":[{"name":"portable-updater-v0.2.0-windows-x64.zip","browser_download_url":"https://github.com/keshon/portable-updater/releases/download/v0.2.0/portable-updater-v0.2.0-windows-x64.zip","size":%d,"digest":%q}]}`, archive.Len(), digest)
		default:
			http.ServeContent(w, r, "package.zip", time.Time{}, bytes.NewReader(archive.Bytes()))
		}
	}))
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // isolated localhost fixture only
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	previousTransport, previousClient := http.DefaultTransport, httpx.Client
	http.DefaultTransport = transport
	httpx.Client = &http.Client{Transport: transport}
	defer func() {
		http.DefaultTransport = previousTransport
		httpx.Client = previousClient
		transport.CloseIdleConnections()
	}()
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Start(ctx, target, "dev", &output); err != nil {
		t.Fatal(err)
	}
	var stage string
	deadline := time.Now().Add(15 * time.Second)
	for {
		paths, _ := filepath.Glob(filepath.Join(root, "tmp/portable-update/self-update/*/result.log"))
		if len(paths) == 1 {
			stage = filepath.Dir(paths[0])
			log, _ := os.ReadFile(paths[0])
			if bytes.Contains(log, []byte("updated to v0.2.0")) {
				break
			}
			if bytes.Contains(log, []byte("failed")) {
				t.Fatalf("helper failed: %s", log)
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("handoff timed out:", output.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	version, err := exec.Command(target, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(version)) != "Portable Updater v0.2.0" {
		t.Fatalf("installed version: %s: %v", version, err)
	}
	installed, _ := files.Digest(target)
	expected, _ := files.Digest(candidate)
	if installed != expected {
		t.Fatal("candidate bytes were not installed")
	}
	config, _ := os.ReadFile(filepath.Join(home, "update.json"))
	if string(config) != "operator configuration; not parsed or changed" {
		t.Fatal("configuration changed")
	}
	for time.Now().Before(deadline) {
		if err := os.Rename(filepath.Join(stage, "helper.exe"), filepath.Join(stage, "helper-finished.exe")); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

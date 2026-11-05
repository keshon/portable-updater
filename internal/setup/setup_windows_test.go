package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/portable-updater/internal/config"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "duckstation/duckstation-qt-x64-ReleaseLTCG.exe"), data)
	for _, rel := range []string{"portable.txt", "settings.ini", "playtime.dat", "Qt6Core.dll", "Qt6Gui.dll", "Qt6Widgets.dll"} {
		put(t, filepath.Join(root, "duckstation", rel), []byte("keep "+rel))
	}
	for _, rel := range []string{"QtPlugins", "resources", "translations"} {
		if err := os.Mkdir(filepath.Join(root, "duckstation", rel), 0755); err != nil {
			t.Fatal(err)
		}
	}
	put(t, filepath.Join(root, "data/memcards/save.mcd"), []byte("my save"))
	put(t, filepath.Join(root, "launch-game.cmd"), []byte("existing launcher"))
	return root, exe
}
func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSetupDetectReviewInstallAndNoOverwrite(t *testing.T) {
	root, exe := fixture(t)
	profiles, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	matches := Detect(root, profiles)
	if len(matches) != 1 || profiles[matches[0]].ID != "duckstation" {
		t.Fatal("did not detect runtime")
	}
	p, err := Prepare(root, "duckstation", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "util")); !os.IsNotExist(err) {
		t.Fatal("review wrote files")
	}
	if len(p.Instance.Saves) != 1 || p.Instance.Saves[0] != "data/memcards" {
		t.Fatal("snapshot defaults do not match existing data")
	}
	if err := p.Install(exe); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(filepath.Join(root, "util/portable-update/update.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Instance.Runtime != "duckstation" {
		t.Fatal("wrong runtime")
	}
	for path, want := range map[string]string{"data/memcards/save.mcd": "my save", "launch-game.cmd": "existing launcher", "duckstation/settings.ini": "keep settings.ini"} {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(b) != want {
			t.Fatalf("changed %s", path)
		}
	}
	if err := p.Install(exe); err == nil {
		t.Fatal("setup overwrote existing installation")
	}
}

func TestAllBundledProfilesLoad(t *testing.T) {
	profiles, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) == 0 {
		t.Fatal("empty bundled catalog")
	}
	for _, profile := range profiles {
		t.Run(profile.ID, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "util/portable-update")
			data, err := json.Marshal(profile.Instance)
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(home, "update.json"), data)
			put(t, filepath.Join(home, profile.Instance.Adapter), profile.AdapterData)
			if _, err := config.Load(filepath.Join(home, "update.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSetupFailureRemovesOnlyOwnFiles(t *testing.T) {
	root, exe := fixture(t)
	put(t, filepath.Join(root, "util/other-tool/keep.txt"), []byte("keep"))
	p, err := Prepare(root, "duckstation", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	original := openSetupFile
	t.Cleanup(func() { openSetupFile = original })
	calls := 0
	openSetupFile = func(path string, flags int, mode os.FileMode) (*os.File, error) {
		calls++
		if calls == 4 {
			return nil, errors.New("disk failure")
		}
		return original(path, flags, mode)
	}
	if err := p.Install(exe); err == nil {
		t.Fatal("injected failure ignored")
	}
	for _, path := range []string{"util/portable-update", "updater-update.cmd", "updater-rollback.cmd"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("partial setup retained %s", path)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "util/other-tool/keep.txt"))
	if err != nil || string(b) != "keep" {
		t.Fatal("changed unrelated utility")
	}
}

func TestSetupRejectsUnsafeOrIncompletePlan(t *testing.T) {
	for _, mode := range []string{"runtime-escape", "snapshot-overlap", "missing-state", "existing-launcher"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := fixture(t)
			runtime := ""
			var saves []string
			switch mode {
			case "runtime-escape":
				runtime = "../duckstation"
			case "snapshot-overlap":
				saves = []string{"tmp"}
			case "missing-state":
				if err := os.Remove(filepath.Join(root, "duckstation/portable.txt")); err != nil {
					t.Fatal(err)
				}
			case "existing-launcher":
				put(t, filepath.Join(root, "updater-update.cmd"), []byte("user script"))
			}
			if _, err := Prepare(root, "duckstation", runtime, saves); err == nil {
				t.Fatal("invalid setup accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "util/portable-update")); !os.IsNotExist(err) {
				t.Fatal("failed review wrote files")
			}
		})
	}
}

func TestWizardCancelAndCustomData(t *testing.T) {
	for _, confirm := range []string{"no", "yes"} {
		t.Run(confirm, func(t *testing.T) {
			root, exe := fixture(t)
			input := strings.NewReader(root + "\nduckstation\n\ndata/memcards\n" + confirm + "\n\n")
			var output bytes.Buffer
			if err := Wizard(input, &output, exe); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(filepath.Join(root, "util/portable-update/update.json"))
			if confirm == "no" && !os.IsNotExist(err) {
				t.Fatal("cancel wrote configuration")
			}
			if confirm == "yes" && err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "Review setup") {
				t.Fatal("missing review")
			}
		})
	}
}

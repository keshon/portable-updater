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

func TestWizardRecognizesRuntimeFolder(t *testing.T) {
	root, exe := fixture(t)
	var output bytes.Buffer
	input := filepath.Join(root, "duckstation") + "\nduckstation\n\n\nno\n"
	if err := Wizard(strings.NewReader(input), &output, exe); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Using its container: "+root) || !strings.Contains(output.String(), "Review setup") {
		t.Fatal(output.String())
	}
	if _, err := os.Stat(filepath.Join(root, "util")); !os.IsNotExist(err) {
		t.Fatal("cancel wrote files")
	}
}

func TestWizardAcceptsFullRuntimePathAndRetriesContainerPath(t *testing.T) {
	root, exe := fixture(t)
	var output bytes.Buffer
	// A container path is a common mistake at step 3; retry without restarting.
	input := root + "\nduckstation\n" + root + "\n" + filepath.Join(root, "duckstation") + "\n\nno\n"
	if err := Wizard(strings.NewReader(input), &output, exe); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "That is the container itself") || !strings.Contains(output.String(), "Review setup") {
		t.Fatal(output.String())
	}
}

func TestWizardExplainsAlreadyConfiguredContainer(t *testing.T) {
	root, exe := fixture(t)
	plan, err := Prepare(root, "duckstation", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = plan.Install(exe); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "util/portable-update/update.json")
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err = Wizard(strings.NewReader(filepath.Join(root, "duckstation")+"\n"), &output, exe); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Already configured") || !strings.Contains(output.String(), "updater-update.cmd") {
		t.Fatal(output.String())
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing configuration changed")
	}
}

func TestSetupFromExecutableInFinalDirectory(t *testing.T) {
	for _, mode := range []string{"install", "cancel", "failure", "different-binary"} {
		t.Run(mode, func(t *testing.T) {
			root, exe := fixture(t)
			initial, err := Prepare(root, "duckstation", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			installedExe := filepath.Join(root, "util/portable-update/portable-update.exe")
			binary, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			put(t, installedExe, binary)
			// Match the executable-only deployment plus existing standard launchers.
			for _, rel := range []string{"updater-update.cmd", "updater-rollback.cmd"} {
				put(t, filepath.Join(root, rel), initial.assets[rel])
			}
			plan, err := Prepare(root, "duckstation", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "cancel":
				var output bytes.Buffer
				err = Wizard(strings.NewReader(root+"\nduckstation\n\n\nno\n"), &output, installedExe)
				if err != nil || !strings.Contains(output.String(), "Review setup") {
					t.Fatalf("%v: %s", err, output.String())
				}
			case "failure":
				original := openSetupFile
				t.Cleanup(func() { openSetupFile = original })
				calls := 0
				openSetupFile = func(path string, flags int, mode os.FileMode) (*os.File, error) {
					calls++
					if calls == 3 {
						return nil, errors.New("injected failure")
					}
					return original(path, flags, mode)
				}
				if err = plan.Install(installedExe); err == nil {
					t.Fatal("injected failure ignored")
				}
			case "different-binary":
				put(t, installedExe, []byte("different executable"))
				if err = plan.Install(exe); !errors.Is(err, ErrSetupConflict) {
					t.Fatalf("different existing binary accepted: %v", err)
				}
			default:
				if err = plan.Install(installedExe); err != nil {
					t.Fatal(err)
				}
				if _, err = config.Load(filepath.Join(root, "util/portable-update/update.json")); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "different-binary" {
				b, e := os.ReadFile(installedExe)
				if e != nil || !bytes.Equal(b, binary) {
					t.Fatal("changed preexisting binary")
				}
			}
			for _, rel := range []string{"updater-update.cmd", "updater-rollback.cmd"} {
				b, e := os.ReadFile(filepath.Join(root, rel))
				if e != nil || !bytes.Equal(b, initial.assets[rel]) {
					t.Fatal("changed preexisting launcher")
				}
			}
			if mode != "install" {
				if _, err = os.Stat(filepath.Join(root, "util/portable-update/update.json")); !os.IsNotExist(err) {
					t.Fatal("unexpected config after cancelled or failed setup")
				}
			}
		})
	}
}

func TestWizardDoesNotRetryDestinationConflictAsRuntime(t *testing.T) {
	root, exe := fixture(t)
	put(t, filepath.Join(root, "updater-update.cmd"), []byte("custom launcher"))
	var output bytes.Buffer
	err := Wizard(strings.NewReader(root+"\nduckstation\n\n"), &output, exe)
	if !errors.Is(err, ErrSetupConflict) || strings.Contains(output.String(), "Cannot use that runtime") {
		t.Fatalf("wrong conflict handling: %v; %s", err, output.String())
	}
}

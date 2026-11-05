package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/keshon/portable-updater/internal/config"
	"github.com/keshon/portable-updater/internal/files"
)

func TestCommonSetupOnlyNeedsLocationAndConfirmation(t *testing.T) {
	for _, choice := range []string{"confirm", "cancel", "eof"} {
		t.Run(choice, func(t *testing.T) {
			root, exe := fixture(t)
			input := root + "\n"
			if choice == "confirm" {
				input += "\n\n"
			} else if choice == "cancel" {
				input += "q\n"
			}
			var out bytes.Buffer
			if err := Wizard(strings.NewReader(input), &out, exe); err != nil {
				t.Fatal(err)
			}
			for _, unwanted := range []string{"Application number", "Application files:", "Snapshot paths", "comma-separated", "\x1b["} {
				if strings.Contains(out.String(), unwanted) {
					t.Fatalf("unexpected prompt %q: %s", unwanted, out.String())
				}
			}
			_, err := os.Stat(filepath.Join(root, "util/portable-update/update.json"))
			if choice == "confirm" {
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), "DuckStation updater is ready.") {
					t.Fatal(out.String())
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("cancel or EOF wrote files")
			}
		})
	}
}

func TestOuterInnerAndExecutableHaveSameReview(t *testing.T) {
	root, exe := fixture(t)
	for _, location := range []string{root, filepath.Join(root, "duckstation"), filepath.Join(root, "duckstation/duckstation-qt-x64-ReleaseLTCG.exe")} {
		var out bytes.Buffer
		if err := Wizard(strings.NewReader(location+"\nq\n"), &out, exe); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Application folder  "+root) || !strings.Contains(out.String(), "Set up DuckStation") {
			t.Fatal(out.String())
		}
		if _, err := os.Stat(filepath.Join(root, "util")); !os.IsNotExist(err) {
			t.Fatal("preview wrote files")
		}
	}
}

func TestAmbiguousLocationRequiresSelection(t *testing.T) {
	root, exe := fixture(t)
	if err := files.CopyTree(filepath.Join(root, "duckstation"), filepath.Join(root, "alternate")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Wizard(strings.NewReader(root+"\n99\n2\nq\n"), &out, exe); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "More than one application") || !strings.Contains(out.String(), "Choose one of the listed numbers") || !strings.Contains(out.String(), "alternate") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "util")); !os.IsNotExist(err) {
		t.Fatal("selection wrote files")
	}
}

func TestEditBackupsAndChannelSurviveBackToReview(t *testing.T) {
	root, exe := fixture(t)
	custom := "data/personal,settings.ini"
	put(t, filepath.Join(root, custom), []byte("keep me"))
	input := strings.Join([]string{root, "4", "invalid", "stable", "3", "r", "1", filepath.Join(root, custom), filepath.Join(root, custom), filepath.Join(root, "data"), "", "", ""}, "\n")
	var out bytes.Buffer
	if err := Wizard(strings.NewReader(input), &out, exe); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(filepath.Join(root, "util/portable-update/update.json"))
	if err != nil {
		t.Fatalf("%v; %s", err, out.String())
	}
	if c.Instance.Channel != "stable" || len(c.Instance.Saves) != 1 || c.Instance.Saves[0] != custom {
		t.Fatalf("edits lost: %+v", c.Instance)
	}
	if !strings.Contains(out.String(), "Cannot add that backup") {
		t.Fatal("duplicate/nested selections were not rejected")
	}
}

func TestManualCustomLayoutKeepsInputOnCorrection(t *testing.T) {
	root, exe := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, "engine"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "duckstation"), filepath.Join(root, "engine/bin")); err != nil {
		t.Fatal(err)
	}
	profiles, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	for i, p := range profiles {
		if p.ID == "duckstation" {
			index = i + 1
		}
	}
	input := strings.Join([]string{root, "p", "bad", strconv.Itoa(index), filepath.Join(root, "missing"), filepath.Join(root, "engine/bin"), "q", ""}, "\n")
	var out bytes.Buffer
	if err := Wizard(strings.NewReader(input), &out, exe); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Cannot use those application files") || !strings.Contains(out.String(), "Application files   engine\\bin") {
		t.Fatal(out.String())
	}
}

func TestSelfHostedSetupNeedsOnlyConfirmation(t *testing.T) {
	root, exe := fixture(t)
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, "util/portable-update/portable-update.exe")
	put(t, installed, binary)
	var out bytes.Buffer
	if err = Wizard(strings.NewReader("\n\n"), &out, installed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Application location (") || !strings.Contains(out.String(), "DuckStation updater is ready.") {
		t.Fatal(out.String())
	}
	if _, err = config.Load(filepath.Join(root, "util/portable-update/update.json")); err != nil {
		t.Fatal(err)
	}
}

func TestBackFromManualEditKeepsReview(t *testing.T) {
	root, exe := fixture(t)
	var out bytes.Buffer
	if err := Wizard(strings.NewReader(root+"\np\nb\nq\n"), &out, exe); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "Set up DuckStation") != 2 {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "util")); !os.IsNotExist(err) {
		t.Fatal("back/cancel wrote files")
	}
}

func TestFilesFolderCanBeChangedDirectlyWithoutChoosingTypeAgain(t *testing.T) {
	root, exe := fixture(t)
	if err := files.CopyTree(filepath.Join(root, "duckstation"), filepath.Join(root, "alternate")); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "duckstation/duckstation-qt-x64-ReleaseLTCG.exe")
	input := strings.Join([]string{entry, "4", "nightly", "2", filepath.Join(root, "alternate"), "", ""}, "\n")
	var out bytes.Buffer
	if err := Wizard(strings.NewReader(input), &out, exe); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Application number") || strings.Contains(out.String(), "Edit choices") {
		t.Fatal("extra navigation returned", out.String())
	}
	c, err := config.Load(filepath.Join(root, "util/portable-update/update.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Instance.Runtime != "alternate" || c.Instance.Channel != "nightly" || len(c.Instance.Saves) != 1 {
		t.Fatalf("choices lost: %+v", c.Instance)
	}
}

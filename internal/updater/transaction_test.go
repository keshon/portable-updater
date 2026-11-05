package updater

import (
	"archive/zip"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/portable-updater/internal/files"
)

func TestFailedSwapRestoresCurrentRuntime(t *testing.T) {
	u, source := fixture(t)
	if e := u.stage(source); e != nil {
		t.Fatal(e)
	}
	stage := filepath.Join(u.root, u.state.Stage)
	original := renamePath
	renamePath = func(from, to string) error {
		if from == stage && to == u.runtime() {
			return errors.New("injected candidate rename failure")
		}
		return original(from, to)
	}
	t.Cleanup(func() { renamePath = original })
	if e := u.apply(false); e == nil {
		t.Fatal("injected swap failure accepted")
	}
	if read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
		t.Fatal("old runtime was not automatically restored")
	}
	if u.state.Phase != "staged" {
		t.Fatal("failed swap journal is not retryable")
	}
	if read(t, filepath.Join(u.root, "data/saves/progress")) != "save" {
		t.Fatal("failed swap changed saves")
	}
}

func TestPreservationCannotRemoveRequiredCandidateFiles(t *testing.T) {
	for _, kind := range []string{"portable-state", "package-file"} {
		t.Run(kind, func(t *testing.T) {
			u, source := fixture(t)
			u.policy.Preserve = []string{"config"}
			if kind == "portable-state" {
				u.policy.RequiredState = []string{"portable.ini"}
			} else {
				put(t, filepath.Join(source, "config/mandatory.json"), "new mandatory file")
				u.policy.RequiredPackage = []string{"config/mandatory.json"}
			}
			if err := u.stage(source); err != nil {
				t.Fatal(err)
			}
			if err := u.apply(false); err == nil || !strings.Contains(err.Error(), "required file after settings preservation") {
				t.Fatal("missing post-preservation state accepted:", err)
			}
			if read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
				t.Fatal("invalid candidate replaced active runtime")
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

func fixture(t *testing.T) (*transaction, string) {
	t.Helper()
	root := t.TempDir()
	u := &transaction{root: root, home: filepath.Join(root, "util", "portable-update"), work: filepath.Join(root, "tmp", "portable-update"), policy: installPolicy{Name: "Fixture", Runtime: "emulator", Executable: "emu.exe", Preserve: []string{"config", "portable.ini"}, Saves: []string{"data/saves"}}}
	put(t, filepath.Join(root, "emulator/emu.exe"), "old")
	put(t, filepath.Join(root, "emulator/stale.dll"), "old DLL")
	put(t, filepath.Join(root, "emulator/config/settings.ini"), "initial config")
	put(t, filepath.Join(root, "emulator/portable.ini"), "")
	put(t, filepath.Join(root, "data/saves/progress"), "save")
	src := t.TempDir()
	put(t, filepath.Join(src, "emu.exe"), "new")
	put(t, filepath.Join(src, "new.dll"), "new DLL")
	put(t, filepath.Join(src, "config/settings.ini"), "upstream default")
	return u, src
}

func TestStageApplyRollback(t *testing.T) {
	u, src := fixture(t)
	if e := u.stage(src); e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
		t.Fatal("stage modified runtime")
	}
	put(t, filepath.Join(u.runtime(), "config/settings.ini"), "played after staging")
	if e := u.apply(false); e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(u.runtime(), "emu.exe")) != "new" {
		t.Fatal("candidate not applied")
	}
	if files.Exists(filepath.Join(u.runtime(), "stale.dll")) {
		t.Fatal("stale DLL survived clean replacement")
	}
	if read(t, filepath.Join(u.runtime(), "config/settings.ini")) != "played after staging" {
		t.Fatal("live config lost")
	}
	if read(t, filepath.Join(u.root, u.state.Snapshot, "data/saves/progress")) != "save" {
		t.Fatal("snapshot missing")
	}
	put(t, filepath.Join(u.root, "data/saves/progress"), "new progress")
	if e := u.rollback(); e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
		t.Fatal("rollback did not restore executable")
	}
	if read(t, filepath.Join(u.root, "data/saves/progress")) != "new progress" {
		t.Fatal("rollback reverted gameplay")
	}
}

func TestWrappedZipAndSourceOverlap(t *testing.T) {
	u, _ := fixture(t)
	if e := u.stage(u.root); e == nil {
		t.Fatal("overlapping source accepted")
	}
	p := filepath.Join(t.TempDir(), "release.zip")
	f, _ := os.Create(p)
	z := zip.NewWriter(f)
	w, _ := z.Create("release/emu.exe")
	w.Write([]byte("wrapped executable"))
	z.Close()
	f.Close()
	if e := u.stage(p); e != nil {
		t.Fatal(e)
	}
	if read(t, filepath.Join(u.root, u.state.Stage, "emu.exe")) != "wrapped executable" {
		t.Fatal("wrapper not normalized")
	}
}

func TestArchiveKeepsDeclaredNestedEntryPoint(t *testing.T) {
	u, _ := fixture(t)
	u.policy.Executable = "bin/emu.exe"
	put(t, filepath.Join(u.runtime(), "bin/emu.exe"), "old nested executable")
	p := filepath.Join(t.TempDir(), "release.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, content := range map[string]string{"wrapper/bin/emu.exe": "nested executable", "wrapper/resources/data": "resource", "unrelated/emu.exe": "wrong location"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = u.stage(p); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(u.root, u.state.Stage, "bin/emu.exe")) != "nested executable" {
		t.Fatal("nested entry point flattened")
	}
	if read(t, filepath.Join(u.root, u.state.Stage, "resources/data")) != "resource" {
		t.Fatal("runtime root incorrectly selected")
	}
}

func TestRecoverInterruptedApply(t *testing.T) {
	u, src := fixture(t)
	if e := u.stage(src); e != nil {
		t.Fatal(e)
	}
	u.state.Previous = u.runtimeCopy("previous", "test")
	u.state.Phase = "old_moved"
	if e := os.Rename(u.runtime(), filepath.Join(u.root, u.state.Previous)); e != nil {
		t.Fatal(e)
	}
	if e := u.save(); e != nil {
		t.Fatal(e)
	}
	if e := u.recover(); e != nil {
		t.Fatal(e)
	}
	if u.state.Phase != "staged" || read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
		t.Fatal("recovery failed")
	}
}

func TestRecoveryLayouts(t *testing.T) {
	cases := []struct {
		name, phase             string
		active, stage, previous bool
		wantPhase, wantActive   string
		wantError               bool
	}{
		{"apply-old-moved", "old_moved", false, true, true, "staged", "previous", false},
		{"apply-completed", "old_moved", true, false, true, "applied", "current", false},
		{"apply-not-started", "swapping", true, true, false, "staged", "current", false},
		{"rollback-current-moved", "rolling_back", false, true, true, "applied", "displaced", false},
		{"rollback-completed", "rolling_back", true, true, false, "rolled_back", "current", false},
		{"rollback-not-started", "rolling_back", true, false, true, "applied", "current", false},
		{"ambiguous-apply", "swapping", true, true, true, "", "", true},
		{"ambiguous-rollback", "rolling_back", true, true, true, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := fixture(t)
			if err := os.Rename(u.runtime(), filepath.Join(u.root, "unused-runtime")); err != nil {
				t.Fatal(err)
			}
			kind := "next"
			if tc.phase == "rolling_back" {
				kind = "rejected"
			}
			u.state = journal{Phase: tc.phase, Stage: u.runtimeCopy(kind, "matrix"), Previous: u.runtimeCopy("previous", "matrix")}
			if tc.active {
				put(t, filepath.Join(u.runtime(), "emu.exe"), "current")
			}
			if tc.stage {
				value := "candidate"
				if kind == "rejected" {
					value = "displaced"
				}
				put(t, filepath.Join(u.root, u.state.Stage, "emu.exe"), value)
			}
			if tc.previous {
				put(t, filepath.Join(u.root, u.state.Previous, "emu.exe"), "previous")
			}
			if err := u.save(); err != nil {
				t.Fatal(err)
			}
			err := u.recover()
			if tc.wantError {
				if err == nil {
					t.Fatal("ambiguous layout accepted")
				}
				for _, path := range []string{u.runtime(), filepath.Join(u.root, u.state.Stage), filepath.Join(u.root, u.state.Previous)} {
					if !files.Exists(filepath.Join(path, "emu.exe")) {
						t.Fatal("ambiguous recovery moved or removed a copy")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if u.state.Phase != tc.wantPhase || read(t, filepath.Join(u.runtime(), "emu.exe")) != tc.wantActive {
				t.Fatalf("unexpected recovery: %+v", u.state)
			}
		})
	}
}

func TestChildHold(t *testing.T) {
	if os.Getenv("PORTABLE_UPDATE_TEST_CHILD") == "1" {
		time.Sleep(20 * time.Second)
	}
}

func TestRunningRuntimeRefused(t *testing.T) {
	u, src := fixture(t)
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	child := filepath.Join(u.runtime(), "runtime-probe.exe")
	if e = files.CopyFile(binary, child); e != nil {
		t.Fatal(e)
	}
	command := exec.Command(child, "-test.run=^TestChildHold$")
	command.Env = append(os.Environ(), "PORTABLE_UPDATE_TEST_CHILD=1")
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	time.Sleep(200 * time.Millisecond)
	if e = u.stage(src); e == nil || !strings.Contains(e.Error(), "close application") {
		t.Fatalf("running runtime accepted or incorrect failure: %v", e)
	}
}

func TestWorkspaceKeepsContainerRootAndSharedUtilitiesClean(t *testing.T) {
	u, source := fixture(t)
	helper := filepath.Join(u.root, "util", "other-helper.exe")
	put(t, helper, "unrelated utility")
	if err := u.stage(source); err != nil {
		t.Fatal(err)
	}
	if err := u.apply(false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{u.state.Stage, u.state.Previous, u.state.Snapshot} {
		if !strings.HasPrefix(filepath.ToSlash(rel), "tmp/portable-update/") {
			t.Fatalf("generated path outside tmp: %s", rel)
		}
	}
	entries, err := os.ReadDir(u.root)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"emulator": true, "data": true, "tmp": true, "util": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			t.Fatalf("generated root clutter: %s", entry.Name())
		}
	}
	if read(t, helper) != "unrelated utility" {
		t.Fatal("shared utility changed")
	}
	if !files.Within(filepath.Join(u.root, "util", "portable-update"), u.journalPath()) {
		t.Fatal("journal is outside updater home")
	}
	if err := u.rollback(); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(u.runtime(), "emu.exe")) != "old" {
		t.Fatal("rollback did not restore runtime from tmp")
	}
}

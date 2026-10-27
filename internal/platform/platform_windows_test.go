package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/portable-updater/internal/files"
)

func TestWindowsFileVersion(t *testing.T) {
	path, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = FileVersion(path); e == nil {
		t.Fatal("Go test binary without version info unexpectedly accepted")
	}
}
func TestLockReleasedWithoutRemovingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "updater.lock")
	unlock, e := LockUpdater(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = LockUpdater(p); e == nil {
		t.Fatal("second writer accepted")
	}
	unlock()
	again, e := LockUpdater(p)
	if e != nil {
		t.Fatal(e)
	}
	again()
	if !files.Exists(p) {
		t.Fatal("test should leave the harmless lock filename")
	}
}

package platform

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/keshon/portable-updater/internal/files"
)

func TestProcessGuardHelper(t *testing.T) {
	if os.Getenv("PORTABLE_UPDATER_GUARD_CHILD") != "1" {
		return
	}
	fmt.Println("ready")
	bufio.NewReader(os.Stdin).ReadByte()
	os.Exit(0)
}

func TestProcessGuardResolvesShortNameAliases(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Long process guard directory")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	ptr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, _, _ := kernel.NewProc("GetShortPathNameW").Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		t.Skip("short names unavailable")
	}
	alias := syscall.UTF16ToString(buffer[:n])
	if strings.EqualFold(alias, root) {
		t.Skip("volume has no short-name alias")
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	holder := filepath.Join(root, "process-hold.exe")
	if err = files.CopyFile(source, holder); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(holder, "-test.run=^TestProcessGuardHelper$")
	command.Env = append(os.Environ(), "PORTABLE_UPDATER_GUARD_CHILD=1")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); command.Process.Kill(); command.Wait() }()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatal("helper did not start", err)
	}
	if err = GuardProcesses(alias); err == nil || !strings.Contains(err.Error(), "close application") {
		t.Fatal("running process through short alias was missed:", err)
	}
}

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

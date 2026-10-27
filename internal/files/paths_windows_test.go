package files

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestCopyTreeDetectsWindowsShortPathOverlap(t *testing.T) {
	source := filepath.Join(t.TempDir(), "Source directory with a long Windows name")
	put(t, filepath.Join(source, "keep.txt"), "original")
	input, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		t.Fatal(err)
	}
	output := make([]uint16, 32768)
	getShortPathName := syscall.NewLazyDLL("kernel32.dll").NewProc("GetShortPathNameW")
	size, _, callErr := getShortPathName.Call(uintptr(unsafe.Pointer(input)), uintptr(unsafe.Pointer(&output[0])), uintptr(len(output)))
	if size == 0 || size >= uintptr(len(output)) {
		t.Fatalf("GetShortPathNameW: %v", callErr)
	}
	short := syscall.UTF16ToString(output[:size])
	if strings.EqualFold(short, source) {
		t.Skip("volume does not supply short-name aliases")
	}
	longCanonical, err := Canonical(source)
	if err != nil {
		t.Fatal(err)
	}
	shortCanonical, err := Canonical(short)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(longCanonical, shortCanonical) {
		t.Fatalf("aliases resolve differently: %q vs %q", longCanonical, shortCanonical)
	}
	destination := filepath.Join(short, "not-created", "child")
	if err := CopyTree(source, destination); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("short-path destination accepted: %v", err)
	}
	if err := CopyTree(short, filepath.Join(source, "another-child")); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("short-path source accepted: %v", err)
	}
	if Exists(filepath.Join(source, "not-created")) {
		t.Fatal("overlap check created destination")
	}
}

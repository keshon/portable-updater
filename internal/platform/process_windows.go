package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/keshon/portable-updater/internal/files"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var createSnapshot = kernel.NewProc("CreateToolhelp32Snapshot")
var processFirst = kernel.NewProc("Process32FirstW")
var processNext = kernel.NewProc("Process32NextW")
var openProcess = kernel.NewProc("OpenProcess")
var queryImage = kernel.NewProc("QueryFullProcessImageNameW")

type processEntry struct {
	Size, Usage, PID        uint32
	Heap                    uintptr
	Module, Threads, Parent uint32
	Priority                int32
	Flags                   uint32
	Exe                     [260]uint16
}

func GuardProcesses(roots ...string) error {
	names := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if e == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".exe") {
				names[strings.ToLower(d.Name())] = true
			}
			return nil
		})
	}
	h, _, e := createSnapshot.Call(2, 0)
	if h == ^uintptr(0) {
		return fmt.Errorf("process snapshot: %w", e)
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	entry := processEntry{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	ok, _, _ := processFirst.Call(h, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		name := strings.ToLower(syscall.UTF16ToString(entry.Exe[:]))
		if entry.PID != uint32(os.Getpid()) && names[name] {
			ph, _, err := openProcess.Call(0x1000, 0, uintptr(entry.PID))
			if ph == 0 {
				return fmt.Errorf("cannot inspect active %s (PID %d); close it before updating: %v", name, entry.PID, err)
			}
			buf := make([]uint16, 32768)
			n := uint32(len(buf))
			success, _, err := queryImage.Call(ph, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
			syscall.CloseHandle(syscall.Handle(ph))
			if success == 0 {
				return fmt.Errorf("cannot inspect process %d: %v", entry.PID, err)
			}
			path := syscall.UTF16ToString(buf[:n])
			for _, root := range roots {
				r, _ := filepath.Abs(root)
				if files.Within(strings.ToLower(r), strings.ToLower(path)) {
					return fmt.Errorf("close emulator before updating: %s (PID %d)", path, entry.PID)
				}
			}
		}
		ok, _, _ = processNext.Call(h, uintptr(unsafe.Pointer(&entry)))
	}
	return nil
}

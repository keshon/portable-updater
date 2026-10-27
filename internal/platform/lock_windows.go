package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Open an ordinary lock file with no sharing. Windows closes the handle after
// a crash; an existing filename is harmless and never requires manual removal.
func LockUpdater(path string) (func(), error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateFileW")
	handle, _, e := proc.Call(uintptr(unsafe.Pointer(p)), 0xC0000000, 0, 0, 4, 0x80, 0)
	if handle == ^uintptr(0) {
		return nil, fmt.Errorf("another updater is running, or its lock cannot be opened: %v", e)
	}
	return func() { syscall.CloseHandle(syscall.Handle(handle)) }, nil
}

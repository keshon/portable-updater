package console

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

func terminalColor(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	var mode uint32
	kernel := syscall.NewLazyDLL("kernel32.dll")
	get := kernel.NewProc("GetConsoleMode")
	set := kernel.NewProc("SetConsoleMode")
	if result, _, _ := get.Call(f.Fd(), uintptr(unsafe.Pointer(&mode))); result == 0 {
		return false
	}
	result, _, _ := set.Call(f.Fd(), uintptr(mode|4))
	return result != 0
}

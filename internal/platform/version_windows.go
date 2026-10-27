package platform

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var versionDLL = syscall.NewLazyDLL("version.dll")
var versionSize = versionDLL.NewProc("GetFileVersionInfoSizeW")
var versionInfo = versionDLL.NewProc("GetFileVersionInfoW")
var queryVersion = versionDLL.NewProc("VerQueryValueW")

func FileVersion(path string) (string, error) {
	pointer, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return "", e
	}
	var ignored uint32
	size, _, e := versionSize.Call(uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(&ignored)))
	if size == 0 {
		return "", fmt.Errorf("cannot read installed executable version: %v", e)
	}
	if size > 16<<20 {
		return "", errors.New("executable version resource is too large")
	}
	buffer := make([]byte, size)
	ok, _, e := versionInfo.Call(uintptr(unsafe.Pointer(pointer)), 0, size, uintptr(unsafe.Pointer(&buffer[0])))
	if ok == 0 {
		return "", e
	}
	root, _ := syscall.UTF16PtrFromString("\\")
	var value unsafe.Pointer
	var length uint32
	ok, _, e = queryVersion.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&length)))
	if ok == 0 || length < 24 {
		return "", errors.New("file version information is unavailable")
	}
	info := unsafe.Slice((*uint32)(value), length/4)
	if info[0] != 0xFEEF04BD {
		return "", errors.New("invalid version signature")
	}
	return fmt.Sprintf("%d.%d.%d.%d", info[2]>>16, info[2]&65535, info[3]>>16, info[3]&65535), nil
}

func OSVersion() string {
	type osInfo struct {
		Size, Major, Minor, Build, Platform uint32
		ServicePack                         [128]uint16
	}
	info := osInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	result, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&info)))
	if result != 0 {
		return "10.0.0"
	}
	return fmt.Sprintf("%d.%d.%d", info.Major, info.Minor, info.Build)
}

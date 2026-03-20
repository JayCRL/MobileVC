//go:build windows

package runner

import (
	"syscall"
	"unsafe"
)

func windowsShortPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, syscall.MAX_PATH)
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetShortPathNameW")
	r0, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r0 == 0 {
		if callErr != syscall.Errno(0) {
			return "", callErr
		}
		return "", syscall.EINVAL
	}
	return syscall.UTF16ToString(buf[:r0]), nil
}

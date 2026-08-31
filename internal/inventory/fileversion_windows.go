//go:build windows

package inventory

import (
	"fmt"
	"syscall"
	"unsafe"
)

// nativeFileVersion returns the fixed version of an executable through
// GetFileVersionInfo. The stdlib has no version-resource reader, so the
// version APIs are bound here.
func nativeFileVersion(path string) string {
	size, _, _ := nativeAPI.getFileVersionInfoSize.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(path))), 0)
	if size == 0 {
		return ""
	}
	buf := make([]byte, size)
	ok, _, _ := nativeAPI.getFileVersionInfo.Call(
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(path))), 0, uintptr(size), uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return ""
	}
	var handle unsafe.Pointer
	_, _, _ = nativeAPI.verQueryValue.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(`\`))),
		uintptr(unsafe.Pointer(&handle)), uintptr(unsafe.Pointer(&size)))
	if handle == nil {
		return ""
	}
	// VS_FIXEDFILEINFO: FileVersionMS at +8, FileVersionLS at +12.
	info := (*vsFixedFileInfo)(handle)
	if info.signature != 0xFEEF04BD {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		(info.fileVersionMS>>16)&0xffff, info.fileVersionMS&0xffff,
		(info.fileVersionLS>>16)&0xffff, info.fileVersionLS&0xffff)
}

type vsFixedFileInfo struct {
	signature        uint32
	structVersion    uint32
	fileVersionMS    uint32
	fileVersionLS    uint32
	productVersionMS uint32
	productVersionLS uint32
	fileFlagsMask    uint32
	fileFlags        uint32
	fileOS           uint32
	fileType         uint32
	fileSubtype      uint32
	fileDateMS       uint32
	fileDateLS       uint32
}

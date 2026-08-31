//go:build windows

package app

import (
	"fmt"
	"syscall"
	"unsafe"
)

type shellExecuteInfoW struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIconOrMon   uintptr
	hProcess     uintptr
}

const (
	seeMaskNoCloseProcess = 0x00000040
	waitFailed            = 0xFFFFFFFF
	infinite              = 0xFFFFFFFF
)

var (
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procShellExecuteEx      = shell32.NewProc("ShellExecuteExW")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess  = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
)

func relaunchElevatedNative(exe, args string) (int, error) {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	params, _ := syscall.UTF16PtrFromString(args)

	info := &shellExecuteInfoW{}
	info.cbSize = uint32(unsafe.Sizeof(*info))
	info.fMask = seeMaskNoCloseProcess
	info.lpVerb = verb
	info.lpFile = file
	info.lpParameters = params
	info.nShow = 1 // SW_SHOWNORMAL

	r, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(info)))
	if r == 0 {
		return 0, fmt.Errorf("ShellExecuteExW failed: %v", callErr)
	}
	if info.hProcess == 0 {
		return 0, fmt.Errorf("ShellExecuteExW did not return a process handle")
	}
	defer procCloseHandle.Call(info.hProcess)

	waitResult, _, _ := procWaitForSingleObject.Call(info.hProcess, infinite)
	if waitResult == waitFailed {
		return 0, fmt.Errorf("WaitForSingleObject failed")
	}
	var exitCode uint32
	gr, _, _ := procGetExitCodeProcess.Call(info.hProcess, uintptr(unsafe.Pointer(&exitCode)))
	if gr == 0 {
		return 0, fmt.Errorf("GetExitCodeProcess failed")
	}
	return int(exitCode), nil
}

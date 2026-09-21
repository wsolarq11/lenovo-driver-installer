//go:build windows

package trust

import (
	"fmt"
	"syscall"
	"unsafe"
)

// VerifyFileSignature validates the Authenticode signature of a file through
// WinVerifyTrust with WTD_UI_NONE. Signed drivers can carry the signature
// either embedded in a PE file or through a catalog referenced by an INF.
var (
	wintrust           = syscall.NewLazyDLL("wintrust.dll")
	procWinVerifyTrust = wintrust.NewProc("WinVerifyTrust")
)

const (
	wtdUIChoiceNone      = 2
	wtdRevocationNone    = 0
	wtdChoiceFile        = 1
	wtdStateActionVerify = 1
	wtdStateActionClose  = 2
)

// winTrustActionGenericVerifyV2 is the standard action GUID
// {00AAC56B-CD44-11D0-8CC2-00C04FC295EE}.
var winTrustActionGenericVerifyV2 = syscall.GUID{
	Data1: 0x00AAC56B,
	Data2: 0xCD44,
	Data3: 0x11D0,
	Data4: [8]byte{0x8C, 0xC2, 0x00, 0xC0, 0x4F, 0xC2, 0x95, 0xEE},
}

// winTrustFileInfo mirrors WINTRUST_FILE_INFO. Fields are pointers on 64-bit;
// Go applies the same alignment as the C ABI, and cbStruct is sized at runtime.
type winTrustFileInfo struct {
	cbStruct       uint32
	pcwszFilePath  uintptr
	hFile          uintptr
	pgKnownSubject uintptr
}

// winTrustData mirrors WINTRUST_DATA. The union member is represented by pFile
// because VerifyFileSignature only uses the file choice.
type winTrustData struct {
	cbStruct            uint32
	pPolicyCallbackData uintptr
	pSIPClientData      uintptr
	dwUIChoice          uint32
	fdwRevocationChecks uint32
	dwUnionChoice       uint32
	pFile               uintptr
	dwStateAction       uint32
	hWVTStateData       uintptr
	pwszURLReference    uintptr
	dwProvFlags         uint32
	dwUIContext         uint32
	pSignatureSettings  uintptr
}

// VerifyFileSignature returns nil when WinVerifyTrust accepts the file, or an
// error describing the failure (no signature, untrusted chain, missing file).
func VerifyFileSignature(path string) error {
	utf16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	fileInfo := winTrustFileInfo{
		cbStruct:      uint32(unsafe.Sizeof(winTrustFileInfo{})),
		pcwszFilePath: uintptr(unsafe.Pointer(utf16)),
	}
	data := winTrustData{
		cbStruct:            uint32(unsafe.Sizeof(winTrustData{})),
		dwUIChoice:          wtdUIChoiceNone,
		fdwRevocationChecks: wtdRevocationNone,
		dwUnionChoice:       wtdChoiceFile,
		pFile:               uintptr(unsafe.Pointer(&fileInfo)),
		dwStateAction:       wtdStateActionVerify,
	}
	r, _, _ := procWinVerifyTrust.Call(
		0,
		uintptr(unsafe.Pointer(&winTrustActionGenericVerifyV2)),
		uintptr(unsafe.Pointer(&data)),
	)
	// The close action must run even when verification failed so the trust
	// provider can release its state data.
	closeData := data
	closeData.dwStateAction = wtdStateActionClose
	_, _, _ = procWinVerifyTrust.Call(
		0,
		uintptr(unsafe.Pointer(&winTrustActionGenericVerifyV2)),
		uintptr(unsafe.Pointer(&closeData)),
	)
	if r != 0 {
		return fmt.Errorf("Authenticode verification failed: 0x%08X", uint32(r))
	}
	return nil
}

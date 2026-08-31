//go:build windows

package inventory

import (
	"fmt"
	"syscall"
	"unsafe"
)

// readSystemFirmwareTableSMBIOS returns the raw RSMB firmware table.
func readSystemFirmwareTableSMBIOS() ([]byte, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetSystemFirmwareTable := kernel32.NewProc("GetSystemFirmwareTable")

	const firmwareTableProviderRSMB = 0x52534D42 // 'RSMB'
	const firmwareTableIDSMBIOS = 0

	size, _, _ := procGetSystemFirmwareTable.Call(firmwareTableProviderRSMB, firmwareTableIDSMBIOS, 0, 0)
	if size == 0 {
		return nil, fmt.Errorf("GetSystemFirmwareTable size call failed")
	}
	buf := make([]byte, size)
	r, _, _ := procGetSystemFirmwareTable.Call(firmwareTableProviderRSMB, firmwareTableIDSMBIOS,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(size))
	if r == 0 {
		return nil, fmt.Errorf("GetSystemFirmwareTable failed")
	}
	return buf, nil
}

// smbiosSerialNumber extracts the system serial from SMBIOS Type 1 (System
// Information), field 0x04, using the SMBIOS 2.x/3.x structure format.
func smbiosSerialNumber(raw []byte) string {
	if len(raw) < 8 {
		return ""
	}
	offset := 8
	for offset+4 <= len(raw) {
		t := raw[offset]
		length := int(raw[offset+1])
		if length < 4 || t == 127 {
			break
		}
		// SMBIOS Type 1 has a fixed 27-byte structure; serial is string 4
		// (offset+7 in the fixed fields).
		if t == 1 && length >= 27 {
			stringsStart := offset + length
			serial := smbiosString(raw, stringsStart, int(raw[offset+7]))
			if serial != "" {
				return serial
			}
		}
		// Advance past this structure's string area. Strings are NUL-
		// separated and the area ends with a double NUL.
		pos := offset + length
		for pos < len(raw) {
			if pos+1 < len(raw) && raw[pos] == 0 && raw[pos+1] == 0 {
				pos += 2
				break
			}
			pos++
		}
		offset = pos
	}
	return ""
}

func smbiosString(raw []byte, start, index int) string {
	if index == 0 {
		return ""
	}
	cur := 1
	pos := start
	for pos < len(raw) {
		end := pos
		for end < len(raw) && raw[end] != 0 {
			end++
		}
		if end == pos {
			break
		}
		if cur == index {
			return decodeASCII(raw[pos:end])
		}
		cur++
		pos = end + 1
	}
	return ""
}

func decodeASCII(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, c)
	}
	return string(out)
}

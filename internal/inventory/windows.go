package inventory

import "strings"

// MachineInfo is the resolved machine identity.
type MachineInfo struct {
	Model  string
	Serial string
}

// OSInfo is the resolved local OS identity.
type OSInfo struct {
	Caption string
	Kind    string
	OSName  string
	Arch    string
}

type osInfoRow struct {
	Caption        string
	OSArchitecture string
}

func normalizeOSInfo(raw osInfoRow) OSInfo {
	kind := "Windows"
	caption := raw.Caption
	switch {
	case strings.Contains(caption, "Windows 11"):
		kind = "Windows 11"
	case strings.Contains(caption, "Windows 10"):
		kind = "Windows 10"
	case strings.Contains(caption, "Windows 8"):
		kind = "Windows 8"
	case strings.Contains(caption, "Windows 7"):
		kind = "Windows 7"
	}
	bits := "32-bit"
	if strings.Contains(raw.OSArchitecture, "64") {
		bits = "64-bit"
	}
	return OSInfo{
		Caption: caption,
		Kind:    kind,
		OSName:  kind + " " + bits,
		Arch:    bits,
	}
}

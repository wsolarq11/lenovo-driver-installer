package model

import (
	"fmt"
	"sort"
	"strings"
)

// DeviceProblemLabel maps a Windows CM_PROB_* device problem code to a short
// human label. Code 0 means the device has no problem. Nonzero codes that are
// not curated here fall back to "unknown problem" rather than a raw numeric.
func DeviceProblemLabel(problem int) string {
	switch problem {
	case 0:
		return ""
	case 10:
		return "failed start"
	case 28:
		return "failed install"
	case 29:
		return "hardware disabled"
	case 31:
		return "failed add"
	case 32:
		return "disabled service"
	case 37:
		return "failed driver entry"
	case 39:
		return "driver failed load"
	case 43:
		return "failed post start"
	case 45:
		return "phantom device"
	case 47:
		return "hold for eject"
	case 48:
		return "driver blocked"
	case 52:
		return "unsigned driver"
	case 54:
		return "device reset"
	default:
		return "unknown problem"
	}
}

// DeviceProblemSummary collapses the distinct problem codes of the given
// devices into one display string, or "" when every device is healthy. The
// output is sorted by problem code so the summary is deterministic.
func DeviceProblemSummary(devices []Device) string {
	seen := map[int]bool{}
	var codes []int
	for _, device := range devices {
		if device.ProblemNumber == 0 || seen[device.ProblemNumber] {
			continue
		}
		seen[device.ProblemNumber] = true
		codes = append(codes, device.ProblemNumber)
	}
	if len(codes) == 0 {
		return ""
	}
	sort.Ints(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("Code %d: %s", code, DeviceProblemLabel(code)))
	}
	return strings.Join(parts, "; ")
}

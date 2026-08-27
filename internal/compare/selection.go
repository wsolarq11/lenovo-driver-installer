package compare

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"lenovo-driver/internal/model"
)

var reSize = regexp.MustCompile(`(?i)^([0-9.]+)\s*(B|KB|MB|GB)?$`)

// ResolveTargetOsEntry mirrors Resolve-TargetOsEntry.
func ResolveTargetOsEntry(osList []model.OSListEntry, targetOS string) *model.OSListEntry {
	target := strings.TrimSpace(targetOS)
	if target == "" {
		return nil
	}
	for i := range osList {
		if strings.EqualFold(osList[i].OSID, target) {
			return &osList[i]
		}
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(target))
	for i := range osList {
		if re.MatchString(osList[i].OSName) {
			return &osList[i]
		}
	}
	return nil
}

// SelectLatestDrivers mirrors Select-LatestDrivers.
func SelectLatestDrivers(drivers []*model.Driver, currentOsID string) []*model.Driver {
	type groupKey struct {
		partID string
		name   string
	}
	groups := map[groupKey][]*model.Driver{}
	for _, d := range drivers {
		if d == nil {
			continue
		}
		key := groupKey{partID: d.PartID, name: d.DriverName}
		groups[key] = append(groups[key], d)
	}
	var selected []*model.Driver
	for _, group := range groups {
		hasCurrent := false
		for _, d := range group {
			if d.OSID == currentOsID {
				hasCurrent = true
				break
			}
		}
		if !hasCurrent {
			continue
		}
		best := group[0]
		for _, d := range group[1:] {
			if driverNewer(d, best) {
				best = d
			}
		}
		selected = append(selected, best)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].PartID < selected[j].PartID
	})
	return selected
}

func driverNewer(a, b *model.Driver) bool {
	c := GetDriverSortVersion(a).Compare(GetDriverSortVersion(b))
	if c != 0 {
		return c > 0
	}
	if !a.IssuedDate.Equal(b.IssuedDate) {
		return a.IssuedDate.After(b.IssuedDate)
	}
	return a.DriverEditionID > b.DriverEditionID
}

// ConvertToBytes mirrors ConvertTo-Bytes.
func ConvertToBytes(sizeText string) int64 {
	sizeText = strings.TrimSpace(sizeText)
	if sizeText == "" {
		return 0
	}
	m := reSize.FindStringSubmatch(sizeText)
	if m == nil {
		plain := int64(0)
		digits := regexp.MustCompile(`[0-9]`).ReplaceAllString(sizeText, "")
		if n, err := strconv.ParseInt(digits, 10, 64); err == nil {
			plain = n
		}
		return plain
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(m[2]) {
	case "KB":
		value *= 1024
	case "MB":
		value *= 1024 * 1024
	case "GB":
		value *= 1024 * 1024 * 1024
	}
	return int64(value)
}

// GetSizeTolerance mirrors Get-SizeTolerance.
func GetSizeTolerance(expectedBytes int64) int64 {
	tolerance := int64(float64(expectedBytes) * 0.02)
	if tolerance < 1024 {
		return 1024
	}
	return tolerance
}

// TestFileSizeMatch mirrors Test-FileSizeMatch.
func TestFileSizeMatch(expectedBytes, actualBytes int64) bool {
	if expectedBytes <= 0 {
		return true
	}
	diff := actualBytes - expectedBytes
	if diff < 0 {
		diff = -diff
	}
	return diff <= GetSizeTolerance(expectedBytes)
}

// TestRebootExitCode mirrors Test-RebootExitCode.
func TestRebootExitCode(exitCode int) bool {
	return exitCode == 3010 || exitCode == 1641
}

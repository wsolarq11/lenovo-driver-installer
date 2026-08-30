package compare

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
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
	for i := range osList {
		if strings.Contains(strings.ToLower(osList[i].OSName), strings.ToLower(target)) {
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
func ConvertToBytes(sizeText string) (int64, error) {
	sizeText = strings.TrimSpace(sizeText)
	if sizeText == "" {
		return 0, nil
	}
	m := reSize.FindStringSubmatch(sizeText)
	if m == nil {
		return 0, fmt.Errorf("invalid file size %q: expected bytes or a B/KB/MB/GB suffix", sizeText)
	}
	value, ok := new(big.Rat).SetString(m[1])
	if !ok {
		return 0, fmt.Errorf("invalid file size %q", sizeText)
	}
	multiplier := int64(1)
	switch strings.ToUpper(m[2]) {
	case "KB":
		multiplier = 1024
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	}
	value.Mul(value, big.NewRat(multiplier, 1))
	return new(big.Int).Quo(value.Num(), value.Denom()).Int64(), nil
}

// GetSizeTolerance mirrors Get-SizeTolerance.
func GetSizeTolerance(expectedBytes int64) int64 {
	tolerance := expectedBytes / 50
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

// InstallSucceeded is the single installer-success predicate. Windows installers
// report success as 0 or a reboot-required code (3010 for WU reboot, 1641 for a
// reboot-then-retry). Owning the policy in one place keeps every installer path
// (MSI, EXE, extracted, interactive) on the same definition.
func InstallSucceeded(exitCode int) bool {
	return exitCode == 0 || TestRebootExitCode(exitCode)
}

package compare

import (
	"regexp"
	"strconv"
	"strings"

	"lenovo-driver/internal/model"
)

var reVersionFragment = regexp.MustCompile(`(\d+(\.\d+){1,6})`)

// Version is a Windows-style 1-4 part version value.
type Version struct {
	Parts []int
}

// String returns the canonical dotted representation.
func (v *Version) String() string {
	if v == nil {
		return ""
	}
	parts := make([]string, len(v.Parts))
	for i, p := range v.Parts {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ".")
}

// Compare returns -1, 0, or 1 by comparing each version part.
func (v *Version) Compare(other *Version) int {
	if v == nil || other == nil {
		if v == nil && other == nil {
			return 0
		}
		if v == nil {
			return -1
		}
		return 1
	}
	n := len(v.Parts)
	if len(other.Parts) > n {
		n = len(other.Parts)
	}
	for i := 0; i < n; i++ {
		a := 0
		b := 0
		if i < len(v.Parts) {
			a = v.Parts[i]
		}
		if i < len(other.Parts) {
			b = other.Parts[i]
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
	}
	return 0
}

// VersionOrder is the outcome of the one comparison this system performs. It
// has four values, not three: Undecided is a distinct outcome and is never
// folded into Less. Folding it in is how an unmeasurable value becomes a fact —
// nil reads as "older than everything", so a driver whose version string
// cannot be parsed would be reported as behind the list and handed to the
// automatic install set. Every decision in the system is a total function on
// these four values, and Undecided is always mapped to the non-acting branch.
type VersionOrder int

const (
	OrderUndecided VersionOrder = iota
	OrderLess
	OrderEqual
	OrderGreater
)

func (o VersionOrder) String() string {
	switch o {
	case OrderLess:
		return "<"
	case OrderEqual:
		return "="
	case OrderGreater:
		return ">"
	default:
		return "undecided"
	}
}

// Order is the single comparison. A missing version yields OrderUndecided
// rather than a direction, so "cannot be measured" can never be mistaken for
// "measured as smaller".
func Order(a, b *Version) VersionOrder {
	if a == nil || b == nil {
		return OrderUndecided
	}
	switch a.Compare(b) {
	case -1:
		return OrderLess
	case 0:
		return OrderEqual
	default:
		return OrderGreater
	}
}

// ResolveComparableVersion reduces a raw version string to the single version
// that both sides of a decision must speak about, or nil when the string is not
// a version at all.
//
// The vendor list carries 49 distinct shapes across the 82JQ rows: bare
// ("31.0.15.4630"), prefixed ("Intel_22.10.0.7"), suffixed
// ("15.11.29.13 MS signed"), composite ("Intel_22.10.0.7/Realtek8852AE_6001.0.
// 10.336/Mediatek_3.0.1.1314") and non-versions ("Inbox"). A composite names
// several packages in one field, so the comparison must first agree on which
// one; comparing a composite against a local version can never succeed, which
// is why callers must not reach for raw string equality here.
func ResolveComparableVersion(raw, vendor string) *Version {
	if raw == "" {
		return nil
	}
	if v := GetMatchingRemoteComponent(raw, vendor); v != nil {
		return v
	}
	return ParseVersionString(raw)
}

// ParseVersionString mirrors Parse-VersionString.
func ParseVersionString(value string) *Version {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "/,") {
		return nil
	}
	clean := value
	if m := reVersionFragment.FindString(value); m != "" {
		clean = m
	}
	partsText := strings.Split(clean, ".")
	if len(partsText) < 1 || len(partsText) > 4 {
		return nil
	}
	parts := make([]int, 0, len(partsText))
	for _, part := range partsText {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil
		}
		parts = append(parts, n)
	}
	return &Version{Parts: parts}
}

// GetDriverSortVersion mirrors the sort version used by latest selection.
func GetDriverSortVersion(driver *model.Driver) *Version {
	if driver != nil {
		if v := ParseVersionString(driver.Version); v != nil {
			return v
		}
	}
	return &Version{Parts: []int{0, 0, 0, 0}}
}

// GetVersionMatchKeys mirrors Get-VersionMatchKeys.
func GetVersionMatchKeys(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var keys []string
	if strings.ContainsAny(text, "/,") {
		for _, part := range reComponentSplit.Split(text, -1) {
			if v := ParseVersionString(part); v != nil {
				keys = append(keys, v.String())
			}
		}
	} else {
		if v := ParseVersionString(text); v != nil {
			keys = append(keys, v.String())
		}
	}
	seen := map[string]bool{}
	unique := keys[:0]
	for _, key := range keys {
		if !seen[key] {
			seen[key] = true
			unique = append(unique, key)
		}
	}
	return unique
}

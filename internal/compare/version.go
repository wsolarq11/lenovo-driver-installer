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
		for _, part := range regexp.MustCompile(`/|,`).Split(text, -1) {
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

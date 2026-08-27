package pathutil

import "strings"

// Base returns the final path element, trimming trailing separators first.
func Base(path string) string {
	path = strings.TrimRight(path, `/\`)
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

// Parent returns the path before the final element, or an empty string.
func Parent(path string) string {
	path = strings.TrimRight(path, `/\`)
	idx := strings.LastIndexAny(path, `/\`)
	if idx < 0 {
		return ""
	}
	return path[:idx]
}

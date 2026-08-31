//go:build !windows

package app

import "fmt"

func relaunchElevatedNative(exe, args string) (int, error) {
	return 0, fmt.Errorf("native elevation requires Windows")
}

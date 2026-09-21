//go:build !windows

package trust

import "fmt"

// VerifyFileSignature is unsupported off Windows. The installer's download and
// install paths only run on Windows, so a non-Windows build never exercises
// this path at runtime.
func VerifyFileSignature(path string) error {
	return fmt.Errorf("Authenticode verification requires Windows")
}

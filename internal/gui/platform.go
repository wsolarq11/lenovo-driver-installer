package gui

// Platform seams: admin check, UAC relaunch, and the silent io.Writer set the
// embedded engine runs behind (the window, not stdout, is the GUI's surface).

import (
	"context"
	"io"
	"strings"

	"lenovo-driver/internal/app"
	"lenovo-driver/internal/inventory"
)

func contextBackground() context.Context { return context.Background() }

func ioDiscard() io.Writer { return io.Discard }

func emptyReader() io.Reader { return strings.NewReader("") }

// isAdmin reports whether the process is elevated.
func isAdmin() (bool, error) {
	return inventory.IsAdministrator()
}

// relaunchRunAs relaunches exe elevated through the UAC prompt and waits for
// its exit code. It reuses the engine's ShellExecuteExW primitive; there is
// one elevation implementation in the repository.
func relaunchRunAs(exe, argLine string) (int, error) {
	return app.RelaunchElevated(exe, argLine)
}

// quoteWindowsArgument quotes one argument by the Windows command-line rules
// the CLI relaunch uses; the GUI's relaunch reuses it.
func quoteWindowsArgument(arg string) string {
	return app.QuoteWindowsArgument(arg)
}

package app

// The GUI exit: the engine face the embedded desktop front end calls. These
// wrappers expose exactly the sequence the CLI's Run dispatches — resolve,
// compare, select-by-codes, install, rollback, pending offers — so the GUI
// adds no second engine path. Anything the GUI needs that is missing here is
// added to this file, never re-derived on the GUI side.

import (
	"context"

	"lenovo-driver/internal/model"
)

// ResolveRuntime resolves machine identity, the OS list, and local snapshots
// into a ViewContext. code is the PS1-style exit code (0 on success); on a
// nonzero code the engine has already logged the reason.
func (a *App) ResolveRuntime(ctx context.Context, opts *Options) (*ViewContext, int) {
	return a.resolveRuntime(ctx, opts)
}

// SelectByCodes resolves an install selection from comma-separated driver
// codes against a built view. code is 0 on success; 3 means the codes did not
// match the current list (unknown or not-applicable), mirroring the CLI.
func (a *App) SelectByCodes(vc *ViewContext, view *DriverView, codes string) ([]*model.AssessedDriver, int) {
	selection := selectByCodes(view.Selected, codes)
	if len(selection.Missing) > 0 || len(selection.NotApplicable) > 0 || len(selection.Selected) == 0 {
		a.Log(context.Background(), "GUI-selected driver codes did not match the current list.", "ERROR")
		return nil, 3
	}
	return selection.Selected, 0
}

// RunRollback performs the pending rollback offers for the comma-separated
// driver codes and returns the PS1-style exit code.
func (a *App) RunRollback(ctx context.Context, codes string) int {
	return a.runRollback(ctx, codes)
}

// QuoteWindowsArgument quotes one argument by the Windows command-line rules
// the CLI relaunch uses. The GUI's UAC relaunch reuses it so there is one
// quoting implementation in the repository.
func QuoteWindowsArgument(arg string) string {
	return quoteWindowsArgument(arg)
}

// RelaunchElevated relaunches the current executable elevated with the given
// arguments (already quoted by QuoteWindowsArgument and joined with spaces)
// and returns its exit code.
func RelaunchElevated(exe, argLine string) (int, error) {
	return relaunchElevatedNative(exe, argLine)
}

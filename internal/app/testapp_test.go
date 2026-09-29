package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// newTestApp returns an App whose every persisted artifact is redirected into
// t.TempDir(). New() deliberately points LogPath/PlanPath/HistoryPath at the
// real per-user audit directory (%LOCALAPPDATA%\Lenovo\DriverInstaller), which
// is the authoritative evidence store on a real machine. A bare New() in a
// test therefore appends fixture lines to the operator's own audit log, where
// they become indistinguishable from a real operation. driverListCacheDir is
// redirected for the same reason.
//
// This is the single constructor for tests: use it instead of New so a future
// artifact path added to New cannot silently start leaking into the real audit
// directory. verify.ps1 enforces that (tests may not call New directly).
func newTestApp(t *testing.T) *App {
	t.Helper()
	app := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	dir := t.TempDir()
	app.LogPath = filepath.Join(dir, "lenovo_driver_install.log")
	app.PlanPath = filepath.Join(dir, "lenovo_driver_plan.txt")
	app.HistoryPath = filepath.Join(dir, "lenovo_driver_history.csv")
	app.driverListCacheDir = dir
	return app
}

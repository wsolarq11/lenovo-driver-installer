package gui

// The updater: a tray-menu check that reports through dialogs. All of it is a
// no-op unless the app was built by `mygo build` with an update feed and key
// (Updater.Enabled): development and unconfigured builds check nothing, so
// the frozen "no hidden network" invariant holds — the check is explicit,
// user-initiated, and disabled by default.

import (
	"context"

	"github.com/egoist/mygo"

	"lenovo-driver/locales"
)

// checkForUpdates asks the update feed once and reports through the app's own
// dialogs. Runs on a background goroutine; mygo dialogs may be called from
// any goroutine.
func checkForUpdates() {
	go func() {
		if !mygo.Updater.Enabled() {
			// No feed configured (personal build): say so rather than
			// pretending to have checked.
			mygo.Dialog.Message(mygo.MessageOptions{
				Title:   locales.AppTitle(),
				Message: locales.UpdateDisabled(),
			})
			return
		}
		// No "checking" dialog: it would block the very check it announces,
		// and the outcome dialog follows within a second or two.
		up, err := mygo.Updater.Check(context.Background())
		if err != nil {
			mygo.Dialog.Message(mygo.MessageOptions{
				Title:   locales.AppTitle(),
				Message: locales.UpdateCheckFailed(err.Error()),
			})
			return
		}
		if up == nil {
			mygo.Dialog.Message(mygo.MessageOptions{
				Title:   locales.AppTitle(),
				Message: locales.UpdateUpToDate(mygo.App.Version()),
			})
			return
		}
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Title:   locales.AppTitle(),
			Message: locales.UpdateAvailable(up.Version),
			Detail:  up.Notes,
			Buttons: []string{locales.UpdateInstall(), locales.UpdateLater()},
		})
		if err != nil || res.Button != 0 {
			return
		}
		if err := up.Install(context.Background(), func(downloaded, total int64) {
			// Progress is silent: the dialog above already said what is
			// happening, and a per-percent dialog would spam the operator.
		}); err != nil {
			mygo.Dialog.Message(mygo.MessageOptions{
				Title:   locales.AppTitle(),
				Message: locales.UpdateInstallFailed(err.Error()),
			})
			return
		}
		mygo.App.Relaunch()
	}()
}

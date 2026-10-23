package gui

// The desktop entry: -Gui launches the window (elevating first through one
// UAC prompt), everything else stays the CLI. The engine runs embedded in
// this process; there is no second engine binary or JSON round trip.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"lenovo-driver/internal/app"
	"lenovo-driver/internal/inventory"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lenovo-driver/locales"
)

// Run dispatches on the GUI flag: with it, the process is the desktop front
// end; without it, a packaged build still opens the window (see wantsGUI) and
// every other launch is the CLI.
func Run(args []string) int {
	guiMode, rest, hasElevationFlag := splitGUIArgs(args)
	if !guiMode && !wantsGUI(rest, mygo.App.IsPackaged()) {
		return app.New(os.Stdout, os.Stderr, os.Stdin).Run(args)
	}
	if hasElevationFlag {
		// Relaunched elevated: drop the marker so a further -Gui run cannot
		// loop the elevation, and run the window directly.
		return runWindow(rest)
	}
	return runElevated(rest)
}

// splitGUIArgs reports whether -Gui is present and strips it and -GuiElevated.
func splitGUIArgs(args []string) (gui bool, rest []string, elevated bool) {
	rest = make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "-Gui":
			gui = true
		case "-GuiElevated":
			elevated = true
		default:
			rest = append(rest, arg)
		}
	}
	return gui, rest, elevated
}

// wantsGUI reports whether a launch without -Gui is still the desktop app.
//
// A packaged build (mygo build) links as a Windows GUI program: it has no
// console, so running the interactive CLI there would wait on a stdin nobody
// can type into and write to a handle nobody can read. A shortcut, the
// installer's finish page, and a deep link all start it with no flags of
// their own, so a packaged no-argument launch is the window. A deep link
// reaches this process as an argument and is the window in any build.
//
// Every other launch stays the CLI: a packaged build is the same engine, and
// someone passing -DryRun wants the console build in bin\.
func wantsGUI(args []string, packaged bool) bool {
	if auditURL(args) != "" {
		return true
	}
	return len(args) == 0 && packaged
}

// runElevated relaunches this executable elevated and waits for it. It is the
// single UAC entry for the GUI: the window never runs unelevated, because an
// install pass started from an unelevated window could not elevate per-driver.
func runElevated(args []string) int {
	admin, err := inventory.IsAdministrator()
	if err == nil && admin {
		return runWindow(args)
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: could not locate the executable:", err)
		return 1
	}
	relaunch := append([]string{"-Gui", "-GuiElevated"}, args...)
	quoted := make([]string, len(relaunch))
	for i, arg := range relaunch {
		quoted[i] = app.QuoteWindowsArgument(arg)
	}
	code, err := app.RelaunchElevated(exe, strings.Join(quoted, " "))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: elevation failed:", err)
		return 1
	}
	return code
}

// runWindow builds the app, the runner, and the window, and runs the event
// loop until the window closes. The window is created in WhenReady, where
// mygo requires it; the auto-refresh job starts after it exists.
func runWindow(args []string) int {
	// One window, one tray, one engine: a second launch hands its command
	// line to this instance (a deep link reaches OnOpenURL there) and exits.
	// The lock belongs here, not in runElevated: the unelevated launcher
	// exits after handing over, and holding the lock across the elevation
	// would make the elevated process find it taken.
	if !mygo.App.RequestSingleInstanceLock() {
		return 0
	}

	a := app.New(io.Discard, io.Discard, strings.NewReader(""))
	s := newGUIState()
	r := newEngineRunner(a, func(fn func(*state)) {
		if win != nil {
			win.Update(func() { fn(s) })
		}
	})
	r.Serve(s)

	// The app is the handler of lenovo-driver:// URLs for the current user;
	// registering again is cheap, so it happens on every launch. The scheme
	// itself is declared in mygo.json (urlSchemes) for `mygo build`.
	if err := mygo.App.RegisterURLScheme(urlScheme); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: url scheme registration failed:", err)
	}

	// A quit is a quit: the close handler below hides the window instead of
	// closing it, and only this hook can tell the two apart before mygo asks
	// the window to close. Without it App.Quit() is cancelled by that very
	// handler and the process outlives its last window with no way back.
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })

	// A second launch brings the window back instead of opening a second
	// one: the tray may have hidden it. Registered before Run so a launch
	// arriving while the app starts is not dropped.
	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Focus()
		}
	})

	// Deep links: lenovo-driver://audit?osid=248 refreshes the list for that
	// OS; lenovo-driver://audit refreshes for the current one. A URL reaches
	// this handler both from the launch arguments and from a later launch
	// forwarded to this instance.
	mygo.App.OnOpenURL(func(u string) {
		go r.Refresh(context.Background(), deeplinkOSID(u), s.IncludeBios, s.SkipHashCheck)
	})

	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.AppTitle(),
			Width:     1240,
			Height:    760,
			MinWidth:  900,
			MinHeight: 500,
			// Remember where the operator put the window across launches.
			StateKey: "main",
			Content: ui.View(func(c *ui.Context) {
				s.view(c)
			}),
		})
		// Show explicitly: a window restored from a hidden state (the tray
		// flow leaves one behind) would otherwise start invisible with no
		// visible way back except the tray.
		win.Show()
		// The real changed hook needs the window, which exists by now.
		s.changed = func() { win.Update(func() {}) }
		// Close-to-tray: a user close hides the window and the tray keeps the
		// app reachable. A quit passes through, see OnBeforeQuit above.
		win.OnClose(func(e *mygo.CloseEvent) {
			if closeHidesWindow(quitting) {
				e.PreventDefault()
				win.Hide()
			}
		})
		// The tray's Quit role calls App.Quit, which OnBeforeQuit marks as a
		// real quit; no separate quit closure is needed.
		if _, err := newTray(
			func() { win.Show() },
			func() {
				s.dispatch(dispatchRequest{kind: runRefresh, includeBios: s.IncludeBios, skipHashCheck: s.SkipHashCheck})
			},
			func() { s.dispatch(dispatchRequest{kind: runOpenPlan}) },
			checkForUpdates,
		); err != nil {
			r.logf("WARN", locales.TrayFailed(err.Error()))
		}
		// Auto-refresh on launch so the header never sits on the placeholder.
		go r.Refresh(context.Background(), "", s.IncludeBios, s.SkipHashCheck)
	})

	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// quitting distinguishes a real quit from a window close that only hides to
// the tray. OnBeforeQuit sets it before mygo closes any window.
var quitting = false

// closeHidesWindow reports whether a window close is the operator hiding the
// window rather than the app quitting. The two are the same event to mygo; a
// quit means the close must go through, or App.Quit() is cancelled by the very
// handler that sends the window to the tray and the process outlives its last
// window with no way back.
func closeHidesWindow(quitting bool) bool { return !quitting }

// win is the window the notify hook posts to. mygo windows are created on the
// main thread and process updates there; the single window is the app's whole
// surface, so the package-level handle is the honest shape.
var win *mygo.Window

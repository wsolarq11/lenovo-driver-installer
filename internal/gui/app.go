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
// end; without it, the CLI runs unchanged. The elevation argument is consumed
// here so the relaunch carries one flag, not the caller's argv.
func Run(args []string) int {
	guiMode, rest, hasElevationFlag := splitGUIArgs(args)
	if !guiMode {
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

	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:  locales.AppTitle(),
			Width:  1240,
			Height: 760,
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
		// app reachable. A quit must pass through, or App.Quit() would hide
		// the window it is trying to close and the process would linger.
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		if _, err := newTray(trayActions{
			ShowWindow: func() { win.Show() },
			Refresh: func() {
				s.dispatch(dispatchRequest{kind: runRefresh, includeBios: s.IncludeBios, skipHashCheck: s.SkipHashCheck})
			},
			OpenPlan: func() { s.dispatch(dispatchRequest{kind: runOpenPlan}) },
			Quit:     quit,
		}); err != nil {
			r.logf("WARN", locales.TrayFailed(err.Error()))
		}
		// Auto-refresh on launch so the header never sits on the placeholder.
		go r.Refresh(context.Background(), "", s.IncludeBios, s.SkipHashCheck)
		// Deep links: lenovo-driver://audit?osid=248 refreshes the list for
		// that OS; lenovo-driver://audit refreshes for the current one. The
		// subscription starts after the window exists, the only place a
		// dispatched job can be rendered into.
		mygo.App.OnOpenURL(func(u string) {
			osid := deeplinkOSID(u)
			go r.Refresh(context.Background(), osid, s.IncludeBios, s.SkipHashCheck)
		})
	})

	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

// quitting distinguishes a real quit (tray menu) from a window close that
// only hides to the tray.
var quitting = false

func quit() {
	quitting = true
	mygo.App.Quit()
}

// win is the window the notify hook posts to. mygo windows are created on the
// main thread and process updates there; the single window is the app's whole
// surface, so the package-level handle is the honest shape.
var win *mygo.Window

package gui

// The tray: a notification-area icon that keeps the app reachable while the
// window is hidden. Its menu carries the same actions as the window, wired
// through the same runner, so the tray adds no second action path.

import (
	_ "embed"

	"github.com/egoist/mygo"

	"lenovo-driver/locales"
)

//go:embed resources/tray.png
var trayIcon []byte

// trayActions carries the closures the tray menu needs. They are the runner's
// own dispatch entry points, shared with the view. It is a struct of required
// functions with no zero value: callers pass it positionally, so a missing
// action is a compile error rather than a nil call at click time. Quit is
// mygo's RoleQuit, which calls App.Quit and so passes through the same
// OnBeforeQuit hook as every other quit.
type trayActions struct {
	ShowWindow func()
	Refresh    func()
	OpenPlan   func()
	CheckForUp func()
}

// newTray builds the tray icon with its menu. Called from WhenReady after the
// window exists. The actions come in order and none may be nil.
func newTray(showWindow, refresh, openPlan, checkForUpdates func()) (*mygo.Tray, error) {
	actions := trayActions{
		ShowWindow: showWindow,
		Refresh:    refresh,
		OpenPlan:   openPlan,
		CheckForUp: checkForUpdates,
	}
	return mygo.NewTray(mygo.TrayOptions{
		ID:      "lenovo-driver",
		Icon:    trayIcon,
		ToolTip: locales.AppTitle(),
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: locales.TrayShowWindow(), Click: func(*mygo.MenuItem, *mygo.Window) { actions.ShowWindow() }},
			{Label: locales.TrayRefresh(), Click: func(*mygo.MenuItem, *mygo.Window) { actions.Refresh() }},
			{Label: locales.TrayOpenPlan(), Click: func(*mygo.MenuItem, *mygo.Window) { actions.OpenPlan() }},
			mygo.Separator(),
			{Label: locales.UpdateMenu(), Click: func(*mygo.MenuItem, *mygo.Window) { actions.CheckForUp() }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}),
	})
}

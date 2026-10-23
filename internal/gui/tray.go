package gui

// The tray: a notification-area icon that keeps the app reachable while the
// window is hidden. Its menu carries the same actions as the window, wired
// through the same runner, so the tray adds no second action path.

import (
	_ "embed"

	"github.com/egoist/mygo"
)

//go:embed resources/tray.png
var trayIcon []byte

// trayActions carries the closures the tray menu needs. They are the runner's
// own dispatch entry points, shared with the view.
type trayActions struct {
	Refresh    func()
	Quit       func()
	ShowWindow func()
	OpenPlan   func()
}

// newTray builds the tray icon with its menu. Called from WhenReady after the
// window exists.
func newTray(actions trayActions) (*mygo.Tray, error) {
	return mygo.NewTray(mygo.TrayOptions{
		ID:      "lenovo-driver",
		Icon:    trayIcon,
		ToolTip: "联想驱动安装器",
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: "显示窗口", Click: func(*mygo.MenuItem, *mygo.Window) { actions.ShowWindow() }},
			{Label: "刷新驱动列表", Click: func(*mygo.MenuItem, *mygo.Window) { actions.Refresh() }},
			{Label: "打开计划文件", Click: func(*mygo.MenuItem, *mygo.Window) { actions.OpenPlan() }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}),
	})
}

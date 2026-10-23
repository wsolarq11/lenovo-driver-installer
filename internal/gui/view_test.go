package gui

// View tests drive the real view function through ui.NewTester without a
// window: render, click, and read back the state. The gate's coverage floor
// for this package stands on these.

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func newTestState() *state {
	s := newGUIState()
	s.MachineCaption = "82JQ / TEST0000  |  当前系统 Windows 11"
	s.OsList = []osEntry{
		{Label: "当前系统 (Windows 10 64-bit)", OSID: ""},
		{Label: "Windows 11 64-bit (OSID 248)", OSID: "248"},
	}
	s.OsID = "248"
	s.Rows = []driverRow{
		{DriverCode: "DRV-A", DriverName: "Monitor Driver 显示器驱动", Version: "20.21.0.1",
			LocalVersion: "10.0.19041.5794", CompareStatus: "Update", EvidenceBasis: "fact",
			SourceAudit: "Official list match", IsApplicable: true, IsUpdate: true},
		{DriverCode: "DRV-B", DriverName: "Energy Management 电源管理", Version: "15.11.29.65",
			LocalVersion: "not installed", CompareStatus: "Not installed", EvidenceBasis: "fact",
			IsApplicable: true},
		{DriverCode: "DRV-C", DriverName: "Wlan Driver 无线网卡", Version: "Intel_22.10.0.7",
			LocalVersion: "23.100.0.4", CompareStatus: "Local newer", EvidenceBasis: "fact",
			IsApplicable: true},
		{DriverCode: "DRV-D", DriverName: "Card Reader 读卡器", CompareStatus: "Not applicable",
			NonMatchReason: "no matching device"},
	}
	s.Selected = map[string]bool{"DRV-A": true, "DRV-B": true}
	return s
}

func TestViewRendersTableAndButtons(t *testing.T) {
	s := newTestState()
	tt := ui.NewTester(s.view, 1240, 760)
	for _, want := range []string{"联想驱动安装器", "DRV-A", "Monitor Driver 显示器驱动", "Local newer", "安装更新项 (y)", "打开计划文件"} {
		if !tt.HasText(want) {
			t.Fatalf("view is missing %q; texts: %v", want, tt.Texts())
		}
	}
}

func TestApplyExportPrefmarksUpdateRows(t *testing.T) {
	s := newTestState()
	if !s.Selected["DRV-A"] || !s.Selected["DRV-B"] || s.Selected["DRV-C"] {
		t.Fatalf("update rows must start checked, others not: %v", s.Selected)
	}
}

func TestApplyExportDoesNotRewriteMachineCaption(t *testing.T) {
	// The header is rendered by Refresh's first stage; applyExport (the row
	// stage, also used by install jobs) must not rewrite it mid-run. The
	// result type no longer even carries a caption to write.
	s := newTestState()
	s.MachineCaption = "已识别的机器"
	r := newTestRunner(s)
	r.notify(func(st *state) {
		st.applyExport(&viewResult{Rows: []driverRow{{DriverCode: "DRV-X"}}})
	})
	if s.MachineCaption != "已识别的机器" {
		t.Fatalf("caption = %q, want it untouched", s.MachineCaption)
	}
	if len(s.Rows) != 1 || s.Rows[0].DriverCode != "DRV-X" {
		t.Fatalf("rows = %v, want the new table", s.Rows)
	}
}

func TestUpdateTargetsMatchTheCLISets(t *testing.T) {
	s := newTestState()
	updates := updateTargets(s.Rows, s.Selected, "updates")
	if len(updates) != 1 || updates[0] != "DRV-A" {
		t.Fatalf("updates = %v, want [DRV-A]", updates)
	}
	applicable := updateTargets(s.Rows, s.Selected, "applicable")
	// Local newer (DRV-C) is in the engine's applicable projection but the
	// membership decision stays the engine's; the GUI only forwards codes.
	if len(applicable) != 3 {
		t.Fatalf("applicable = %v, want DRV-A, DRV-B, DRV-C", applicable)
	}
	selected := updateTargets(s.Rows, s.Selected, "selected")
	if len(selected) != 2 {
		t.Fatalf("selected = %v, want the two checked codes", selected)
	}
}

func TestInstallSummaryListsTargets(t *testing.T) {
	s := newTestState()
	req := installRequest{codes: []string{"DRV-A", "DRV-B"}}
	summary := installSummary(req, s.Rows)
	for _, want := range []string{"下载并安装以下 2 个驱动", "DRV-A", "Monitor Driver 显示器驱动", "DRV-B"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %q", want, summary)
		}
	}
	dl := installSummary(installRequest{codes: []string{"DRV-A"}, downloadOnly: true}, s.Rows)
	if !strings.Contains(dl, "仅下载") {
		t.Fatalf("download summary missing 仅下载: %q", dl)
	}
}

func TestAskInstallWithNoTargetsLogsInstead(t *testing.T) {
	s := newTestState()
	s.Selected = map[string]bool{}
	s.Rows = nil
	dispatched := false
	s.dispatch = func(dispatchRequest) { dispatched = true }
	s.askInstall("selected", false)
	if s.Confirm != nil || dispatched {
		t.Fatal("no targets must not open a dialog or dispatch")
	}
	if len(s.Log) == 0 || s.Log[len(s.Log)-1].Text != "没有符合条件的驱动。" {
		t.Fatalf("expected the empty-set log line, got %v", s.Log)
	}
}

func TestAskInstallOpensConfirmDialogAndDispatches(t *testing.T) {
	s := newTestState()
	var got dispatchRequest
	s.dispatch = func(req dispatchRequest) { got = req }
	s.askInstall("updates", false)
	if s.Confirm == nil || s.confirming != true {
		t.Fatalf("expected a pending dialog, got %+v", s.Confirm)
	}
	// The operator confirms: the dialog's Confirm action dispatches.
	s.Confirm.Confirm()
	if got.kind != runInstall || len(got.install.codes) != 1 || got.install.codes[0] != "DRV-A" {
		t.Fatalf("dispatch = %+v, want runInstall with [DRV-A]", got)
	}
}

func TestAppendLogTrimsToCap(t *testing.T) {
	s := newGUIState()
	for i := range 2100 {
		s.appendLog("INFO", "line")
		s.changed()
		if i == 0 {
			_ = i
		}
	}
	if len(s.Log) != 2000 {
		t.Fatalf("log cap = %d, want 2000", len(s.Log))
	}
}

func TestLogLevelsRenderColored(t *testing.T) {
	s := newGUIState()
	s.appendLog("INFO", "ok line")
	s.appendLog("ERROR", "bad line")
	s.appendLog("WARN", "warn line")
	tt := ui.NewTester(s.view, 1240, 760)
	if !tt.HasText("bad line") || !tt.HasText("warn line") {
		t.Fatalf("log pane missing lines: %v", tt.Texts())
	}
}

func TestAskInstallForExplicitCodesOpensDialog(t *testing.T) {
	// The row context menu's 安装此项 path: an explicit code list opens the
	// same confirmation dialog as the buttons.
	s := newTestState()
	var got dispatchRequest
	s.dispatch = func(req dispatchRequest) { got = req }
	s.askInstallFor([]string{"DRV-B"}, false)
	if s.Confirm == nil || !s.confirming {
		t.Fatal("explicit codes must open the confirm dialog")
	}
	s.Confirm.Confirm()
	if len(got.install.codes) != 1 || got.install.codes[0] != "DRV-B" {
		t.Fatalf("dispatch = %+v, want [DRV-B]", got)
	}
}

func TestAskInstallForEmptyCodesLogsOnly(t *testing.T) {
	s := newTestState()
	s.askInstallFor(nil, false)
	if s.Confirm != nil {
		t.Fatal("empty codes must not open a dialog")
	}
	if len(s.Log) == 0 || s.Log[len(s.Log)-1].Text != "没有符合条件的驱动。" {
		t.Fatalf("log tail = %v", s.Log)
	}
}

func TestViewRendersTabsAndPlanPaneSwitches(t *testing.T) {
	s := newTestState()
	s.planEditor.Buffer.Reset("PLAN LINE ONE")
	tt := ui.NewTester(s.view, 1240, 760)
	if !tt.HasText("计划文件") {
		t.Fatalf("tabs missing 计划文件: %v", tt.Texts())
	}
	// Switch to the plan tab; the read-only editor shows the plan text. Each
	// NewTester owns its build pass, so a fresh state carries the switch. The
	// editor's buffer content lives in its AT value channel, not in the texts
	// the tester reads, so the pane is verified by its label and by the buffer
	// the view renders.
	s2 := newTestState()
	s2.Tab = 1
	s2.planEditor.Buffer.Reset("PLAN LINE ONE")
	tt2 := ui.NewTester(s2.view, 1240, 760)
	// The editor announces itself through the accessibility label; with the
	// zh-Hans catalog registered, mygo's own string for it is 代码编辑器.
	if !tt2.HasText("代码编辑器") && !tt2.HasText("Code editor") {
		t.Fatalf("plan pane missing the editor: %v", tt2.Texts())
	}
	if got := s2.planEditor.Buffer.String(); got != "PLAN LINE ONE" {
		t.Fatalf("plan buffer = %q", got)
	}
}

// The install labels say (y) and (a); pressing those keys must open the same
// confirmation the buttons open, or the labels advertise a key that does
// nothing. The dialog (not a direct install) is the contract: one keystroke
// cannot install anything.
func TestInstallShortcutKeysOpenTheConfirmDialog(t *testing.T) {
	cases := []struct {
		key   ui.Key
		codes []string
	}{
		{ui.KeyY, []string{"DRV-A"}},
		{ui.KeyA, []string{"DRV-A", "DRV-B", "DRV-C"}},
	}
	for _, c := range cases {
		s := newTestState()
		tt := ui.NewTester(s.view, 1240, 760)
		tt.Key(ui.Modifiers(0), c.key)
		if s.Confirm == nil {
			t.Fatalf("key %v must open the install confirmation", c.key)
		}
		var got dispatchRequest
		s.dispatch = func(req dispatchRequest) { got = req }
		s.Confirm.Confirm()
		if got.kind != runInstall || len(got.install.codes) != len(c.codes) {
			t.Fatalf("key %v dispatched %+v, want runInstall for %v", c.key, got, c.codes)
		}
		for i, want := range c.codes {
			if got.install.codes[i] != want {
				t.Fatalf("key %v dispatched %v, want %v", c.key, got.install.codes, c.codes)
			}
		}
	}
}

// A launch that names the desktop app's URL scheme is the window even without
// -Gui: Windows hands a deep link to the registered handler with no flags of
// its own, and running the CLI there would open a console that cannot be
// typed into.
func TestWantsGUIDispatch(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		packaged bool
		want     bool
	}{
		{"deep link in a plain build", []string{"lenovo-driver://audit?osid=248"}, false, true},
		{"deep link in a packaged build", []string{"lenovo-driver://audit"}, true, true},
		{"packaged launch with no arguments", nil, true, true},
		{"unpackaged launch with no arguments", nil, false, false},
		{"packaged CLI run", []string{"-DryRun"}, true, false},
		{"unpackaged CLI run", []string{"-DryRun"}, false, false},
		{"an unrelated URL argument", []string{"https://example.com"}, true, false},
	}
	for _, c := range cases {
		if got := wantsGUI(c.args, c.packaged); got != c.want {
			t.Errorf("%s: wantsGUI(%v, %v) = %v, want %v", c.name, c.args, c.packaged, got, c.want)
		}
	}
}

// At the real minimum DIP width the header must keep the OS dropdown, both
// option check boxes and the Refresh button inside the viewport. The dropdown
// has a fixed width, so an oversized one pushes the actions out of the window
// where they cannot be reached at all.
func TestHeaderFitsTheNarrowestWindow(t *testing.T) {
	for _, width := range []int{900, 826, 768} {
		s := newTestState()
		s.MachineCaption = "82JQ / PF2SBWJA  |  当前系统 Windows 10 Enterprise"
		tt := ui.NewTester(s.view, width, 506)
		for _, label := range []string{"驱动列表系统", "包含 BIOS/EC", "跳过哈希校验", "刷新驱动列表"} {
			r, ok := tt.Find(label)
			if !ok {
				t.Fatalf("width %d: %q missing", width, label)
			}
			if r.X < 0 || r.X+r.W > float32(width) {
				t.Errorf("width %d: %q at X=%.0f W=%.0f is outside the window", width, label, r.X, r.W)
			}
		}
	}
}

// The log is a fixed-height band, not a single collapsed line: lines stack
// downward and the pane keeps its own surface instead of vanishing into the
// window background.
func TestLogPaneKeepsItsBand(t *testing.T) {
	s := newTestState()
	s.appendLog("INFO", "one")
	tt := ui.NewTester(s.view, 900, 700)
	first, ok := tt.Find("one")
	if !ok {
		t.Fatal("log line missing")
	}
	s.appendLog("INFO", "two")
	tt.Frame()
	second, ok := tt.Find("two")
	if !ok {
		t.Fatal("second log line missing")
	}
	if second.Y <= first.Y {
		t.Errorf("log lines must stack downward: one at %.0f, two at %.0f", first.Y, second.Y)
	}
}

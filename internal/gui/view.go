package gui

// The view: state -> mygo native UI, one function per region. Pure over the
// state; widget state (table selection, scroll) lives on the state itself so
// the view rebuilds around it frame to frame.

import (
	"github.com/egoist/mygo/ui"
)

// driverTableColumns names the table's columns in display order.
var driverTableColumns = []ui.TableColumn{
	{Title: "安装", Width: 52, Fixed: true},
	{Title: "驱动代码", Width: 150},
	{Title: "驱动名称", Width: 320},
	{Title: "官方版本", Width: 140},
	{Title: "本地版本", Width: 150},
	{Title: "状态", Width: 110},
	{Title: "证据分级", Width: 90},
	{Title: "来源", Width: 300},
	{Title: "设备问题", Width: 140},
	{Title: "不适用原因", Width: 240},
}

// view builds the window from the state. The mygo-provided default (empty
// state) shows an idle empty window; the real one is assembled by newGUIState.
func (s *state) view(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		s.headerRegion(c)
		if s.Busy {
			ui.Progress(c, -1).FillWidth()
		}
		s.tableRegion(c)
		s.actionRegion(c)
		s.logRegion(c)
	})
	s.confirmRegion(c)
}

// headerRegion builds the machine caption, OS dropdown and option checkboxes.
func (s *state) headerRegion(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(12, 10).Gap(12).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "联想驱动安装器").FontSize(20).Bold()
			ui.Text(c, s.MachineCaption).TextColor(c.Theme().TextMuted)
		}).Grow(1)
		ui.Column(c).Gap(2).Width(320).Children(func() {
			ui.Text(c, "驱动列表系统").TextColor(c.Theme().TextMuted)
			s.osSelect(c)
		})
		ui.Column(c).Gap(2).Children(func() {
			ui.Checkbox(c, &s.IncludeBios, "包含 BIOS/EC")
			ui.Checkbox(c, &s.SkipHashCheck, "跳过哈希校验")
		})
		ui.PrimaryButton(c, "刷新驱动列表").Disabled(s.Busy).OnClick(s.refreshAction)
	})
}

// osSelect builds the OS dropdown. The selection is a string keyed by OSID
// ("" for the current system); ui.Select matches it against the labels, so
// the labels carry the OSID mapping through the same slice.
func (s *state) osSelect(c *ui.Context) {
	options := make([]string, len(s.OsList))
	for i, entry := range s.OsList {
		options[i] = entry.Label
	}
	selected := s.osLabel()
	ui.Select(c, &selected, options).FillWidth().OnChange(func() {
		s.OsID = s.osIDFor(selected)
	})
}

// osLabel returns the dropdown label of the selected OSID.
func (s *state) osLabel() string {
	for _, entry := range s.OsList {
		if entry.OSID == s.OsID {
			return entry.Label
		}
	}
	return ""
}

// osIDFor resolves the OSID of a dropdown label.
func (s *state) osIDFor(label string) string {
	for _, entry := range s.OsList {
		if entry.Label == label {
			return entry.OSID
		}
	}
	return ""
}

// tableRegion builds the driver table.
func (s *state) tableRegion(c *ui.Context) {
	ui.Table(c, &s.listState, driverTableColumns, len(s.Rows), func(row, col int) {
		if row < 0 || row >= len(s.Rows) {
			return
		}
		r := s.Rows[row]
		switch col {
		case 0:
			checked := s.Selected[r.DriverCode]
			if ui.CheckboxBase(c, &checked).Clicked() {
				if checked {
					s.Selected[r.DriverCode] = true
				} else {
					delete(s.Selected, r.DriverCode)
				}
			}
		case 1:
			ui.Text(c, r.DriverCode).SingleLine()
		case 2:
			ui.Text(c, r.DriverName).SingleLine()
		case 3:
			ui.Text(c, r.Version).SingleLine()
		case 4:
			ui.Text(c, r.LocalVersion).SingleLine()
		case 5:
			ui.Text(c, r.CompareStatus).SingleLine().TextColor(statusColor(c, r.CompareStatus))
		case 6:
			ui.Text(c, r.EvidenceBasis).SingleLine().TextColor(c.Theme().TextMuted)
		case 7:
			ui.Text(c, r.SourceAudit).SingleLine().TextColor(c.Theme().TextMuted)
		case 8:
			ui.Text(c, r.DeviceProblem).SingleLine().TextColor(c.Theme().TextMuted)
		case 9:
			ui.Text(c, r.NonMatchReason).SingleLine().TextColor(c.Theme().TextMuted)
		}
	}).Grow(1)
}

// statusColor colors a compare status by its evidence class: danger for the
// statuses that mean the device is behind, warning for the ones that need a
// reader's attention, muted text otherwise.
func statusColor(c *ui.Context, status string) ui.Color {
	theme := c.Theme()
	switch status {
	case "Update", "Not installed":
		return theme.Accent
	case "Local newer":
		return theme.Warning
	case "Not applicable":
		return theme.TextMuted
	default:
		return theme.Text
	}
}

// actionRegion builds the install/download buttons and the run status.
func (s *state) actionRegion(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(10, 8).Gap(10).Children(func() {
		s.installButton(c, "安装更新项 (y)", "updates")
		s.installButton(c, "安装需更新项 (a)", "applicable")
		s.installButton(c, "安装选中项", "selected")
		s.downloadButton(c)
		ui.Button(c, "打开计划文件").Disabled(s.Busy).OnClick(s.openPlanAction)
		ui.Spacer(c).Grow(1)
		status := "就绪"
		if s.Busy {
			status = "正在运行 Lenovo 驱动引擎..."
		}
		ui.Text(c, status).TextColor(c.Theme().TextMuted)
	})
}

// installButton opens the install confirmation for one target set.
func (s *state) installButton(c *ui.Context, label, kind string) {
	ui.Button(c, label).Disabled(s.Busy).OnClick(func() { s.askInstall(kind, false) })
}

// downloadButton opens the download-only confirmation for the checked rows.
func (s *state) downloadButton(c *ui.Context) {
	ui.Button(c, "仅下载选中项").Disabled(s.Busy).OnClick(func() { s.askInstall("selected", true) })
}

// askInstall resolves the target codes for one button and opens the
// confirmation dialog; with nothing to act on it logs instead.
func (s *state) askInstall(kind string, downloadOnly bool) {
	if s.Busy {
		return
	}
	codes := updateTargets(s.Rows, s.Selected, kind)
	if len(codes) == 0 {
		s.appendLog("INFO", "没有符合条件的驱动。")
		s.changed()
		return
	}
	req := installRequest{codes: codes, downloadOnly: downloadOnly}
	s.Confirm = &confirmDialog{
		Title:   "确认驱动集合",
		Message: installSummary(req, s.Rows),
		Confirm: func() { s.dispatchInstall(req) },
	}
	s.confirming = true
	s.changed()
}

// logRegion builds the read-only run log pane.
func (s *state) logRegion(c *ui.Context) {
	s.logListState.FollowEnd = true
	ui.Column(c).Height(170).FillWidth().Children(func() {
		ui.List(c, &s.logListState, len(s.Log), func(i int) {
			line := s.Log[i]
			color := c.Theme().Text
			switch line.Level {
			case "ERROR":
				color = c.Theme().Danger
			case "WARN":
				color = c.Theme().Warning
			}
			ui.Text(c, line.Text).SingleLine().TextColor(color)
		}).FillWidth()
	}).Background(c.Theme().Inverse)
}

// confirmRegion builds the pending confirmation dialog, if any.
func (s *state) confirmRegion(c *ui.Context) {
	if s.Confirm == nil {
		return
	}
	switch ui.AlertDialog(c, &s.confirming, s.Confirm.Title, s.Confirm.Message, "取消", "确定") {
	case 1:
		confirm := s.Confirm
		s.Confirm = nil
		confirm.Confirm()
	case ui.AlertDismissed:
		s.Confirm = nil
	}
}

// openPlanAction opens the engine's plan file in the default editor.
func (s *state) openPlanAction() {
	s.dispatch(dispatchRequest{kind: runOpenPlan})
}

// Package gui is the desktop front end: a mygo native-UI window over the
// embedded engine (internal/app). The engine owns every driver decision; this
// package renders its results and forwards operator intent, so it must never
// re-derive a comparison, a source audit, or an install-set membership.
package gui

import (
	"github.com/egoist/mygo/ui"
)

// logLine is one entry of the run log pane, colored by level.
type logLine struct {
	Level string
	Text  string
}

// driverRow is one row of the driver table. It is the GUI-facing projection of
// model.AssessedDriver: presentation strings only, every decision field read
// from the engine's assessment, none recomputed here.
type driverRow struct {
	DriverCode     string
	DriverName     string
	Version        string
	LocalVersion   string
	CompareStatus  string
	SourceAudit    string
	EvidenceBasis  string
	DeviceProblem  string
	NonMatchReason string
	FileName       string
	FileSize       string
	IsApplicable   bool
	IsUpdate       bool
}

// osEntry is one entry of the OS dropdown.
type osEntry struct {
	Label string
	OSID  string
}

// confirmDialog is a pending yes/no question. Answer drives the action the
// dialog was opened for; nil while no dialog shows.
type confirmDialog struct {
	Title   string
	Message string
	// Confirm is the action run when the operator answers yes.
	Confirm func()
}

// runKind names the background job the runner is executing.
type runKind int

const (
	runNone runKind = iota
	runRefresh
	runInstall
	runRollback
	runOpenPlan
)

// installRequest is one confirmed install/download action.
type installRequest struct {
	codes        []string
	downloadOnly bool
}

// state is the GUI's durable state. The view is a function of it; the runner
// mutates it only on the main thread (mygo Window.Update), and widget state
// (table scroll, log follow) lives here so rebuilds keep it.
type state struct {
	// machine caption in the header, e.g. "82JQ / PF2XXXXX | 当前系统 ...".
	MachineCaption string

	// OsList populates the dropdown; OsID is the selected OSID ("" selects
	// the current system).
	OsList []osEntry
	OsID   string

	Rows []driverRow

	Log []logLine

	// Busy is whether a background job runs. Busy disables every action
	// button and shows the indeterminate progress bar.
	Busy    bool
	Running runKind

	IncludeBios   bool
	SkipHashCheck bool

	// Selected holds the checked rows by driver code.
	Selected map[string]bool

	// Confirm is the pending dialog, nil when none shows; confirming is the
	// dialog's open flag (AlertDialog's *bool).
	Confirm    *confirmDialog
	confirming bool

	// changed asks the host for a redraw; the window installs the hook.
	changed func()

	// widget state
	listState    ui.ListState
	logListState ui.ListState

	// dispatch carries confirmed actions to the window, which binds them to
	// the runner; nil in view-only tests.
	dispatch func(req dispatchRequest)
}

// dispatchRequest is one action the view asks the host to run.
type dispatchRequest struct {
	kind          runKind
	osID          string
	install       installRequest
	includeBios   bool
	skipHashCheck bool
}

// newGUIState returns the initial state with redraw and dispatch hooks unset;
// the window assembly installs them.
func newGUIState() *state {
	return &state{
		Selected: map[string]bool{},
		changed:  func() {},
		dispatch: func(dispatchRequest) {},
	}
}

// refreshAction forwards the refresh button to the host.
func (s *state) refreshAction() {
	s.dispatch(dispatchRequest{kind: runRefresh, osID: s.OsID, includeBios: s.IncludeBios, skipHashCheck: s.SkipHashCheck})
}

// dispatchInstall forwards a confirmed install to the host.
func (s *state) dispatchInstall(req installRequest) {
	s.dispatch(dispatchRequest{
		kind:          runInstall,
		osID:          s.OsID,
		install:       req,
		includeBios:   s.IncludeBios,
		skipHashCheck: s.SkipHashCheck,
	})
}

// updateTargets resolves the install set for one of the three install buttons:
// the update-only set, the automatic install set, or the checked rows. It
// applies the same model.InAutomaticInstallSet predicate the CLI's automatic
// set uses, read off the engine's own assessment, so the GUI never widens it.
func updateTargets(rows []driverRow, selected map[string]bool, kind string) []string {
	var codes []string
	for _, row := range rows {
		switch kind {
		case "updates":
			if row.IsUpdate {
				codes = append(codes, row.DriverCode)
			}
		case "applicable":
			if row.IsApplicable {
				codes = append(codes, row.DriverCode)
			}
		case "selected":
			if selected[row.DriverCode] {
				codes = append(codes, row.DriverCode)
			}
		}
	}
	return codes
}

// installSummary renders the confirmation dialog body for one install batch.
func installSummary(req installRequest, rows []driverRow) string {
	byCode := make(map[string]driverRow, len(rows))
	for _, row := range rows {
		byCode[row.DriverCode] = row
	}
	action := "下载并安装"
	if req.downloadOnly {
		action = "仅下载"
	}
	summary := action + "以下 " + itoa(len(req.codes)) + " 个驱动："
	for _, code := range req.codes {
		if row, ok := byCode[code]; ok {
			summary += "\n" + code + "  " + row.DriverName + "  " + row.Version + " -> " + row.LocalVersion
		}
	}
	return summary
}

// appendLog appends one line, trimming the pane at a fixed window so a long
// run cannot grow the state without bound.
func (s *state) appendLog(level, text string) {
	s.Log = append(s.Log, logLine{Level: level, Text: text})
	const logCap = 2000
	if len(s.Log) > logCap {
		s.Log = s.Log[len(s.Log)-logCap:]
	}
	s.changed()
}

// applyExport replaces the table and dropdown from an engine view result.
// The machine caption is not touched here: Refresh renders the header in its
// earlier stage, and Install jobs must not rewrite it mid-run.
func (s *state) applyExport(v *viewResult) {
	s.Rows = v.Rows[:len(v.Rows):len(v.Rows)]
	s.OsList = v.OsList
	s.OsID = v.ListOsID
	s.Selected = map[string]bool{}
	for _, row := range s.Rows {
		if row.IsUpdate {
			s.Selected[row.DriverCode] = true
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

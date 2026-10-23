package gui

// The runner: engine calls on background goroutines, state updates back on
// the main thread. Every engine call here is the same one the CLI's Run
// dispatches to, so the GUI adds no second engine path.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"

	"lenovo-driver/internal/app"
	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// viewResult is one refresh outcome applied to the state on the main thread.
type viewResult struct {
	MachineCaption string
	OsList         []osEntry
	ListOsID       string
	Rows           []driverRow
}

// engineRunner owns the embedded engine and the single background job slot.
// Jobs are serialized: a second action while one runs is refused in the view
// (buttons disabled) and again here, so the invariant is not load-bearing.
type engineRunner struct {
	mu   sync.Mutex
	busy bool

	app    *app.App
	notify func(fn func(s *state))
}

// newEngineRunner wraps the engine. notify schedules a state mutation on the
// UI thread (mygo Window.Update).
func newEngineRunner(a *app.App, notify func(fn func(s *state))) *engineRunner {
	return &engineRunner{app: a, notify: notify}
}

// logf appends one engine log line to the state.
func (r *engineRunner) logf(level, text string) {
	r.notify(func(s *state) { s.appendLog(level, text) })
}

// start refuses a second job and marks the state busy.
func (r *engineRunner) start(kind runKind) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy {
		return false
	}
	r.busy = true
	r.notify(func(s *state) { s.Busy, s.Running = true, kind })
	return true
}

// done clears the busy flag.
func (r *engineRunner) done() {
	r.mu.Lock()
	r.busy = false
	r.mu.Unlock()
	r.notify(func(s *state) { s.Busy, s.Running = false, runNone })
}

// Serve wires the runner to the state: the view's dispatch requests become
// engine jobs on background goroutines. It returns the state the window
// renders.
func (r *engineRunner) Serve(s *state) *state {
	s.dispatch = func(req dispatchRequest) {
		ctx := context.Background()
		switch req.kind {
		case runRefresh:
			go r.Refresh(ctx, req.osID, req.includeBios, req.skipHashCheck)
		case runInstall:
			go r.Install(ctx, req.osID, req.install, req.includeBios, req.skipHashCheck)
		case runRollback:
			go r.Rollback(ctx, req.install.codes)
		case runOpenPlan:
			go r.OpenPlan()
		}
	}
	return s
}

// Refresh resolves machine identity, builds the comparison view for osID
// ("" selects the current system), and applies it to the state. It is the
// CLI's resolveRuntime + resolveListOS + CompareOSDriverView sequence,
// rendered in two stages: the machine identity and OS dropdown appear as soon
// as resolution lands (the state renders an empty table meanwhile), and the
// rows follow when the comparison view is built. Every value still comes from
// one live run; nothing is cached or reused across runs.
func (r *engineRunner) Refresh(ctx context.Context, osID string, includeBios, skipHashCheck bool) {
	if !r.start(runRefresh) {
		return
	}
	defer r.done()
	vc, listOsID, err := r.resolve(ctx, osID, includeBios, skipHashCheck)
	if err != nil {
		return
	}
	r.notify(func(s *state) {
		s.MachineCaption = machineCaption(vc)
		s.OsList = osEntries(vc.OsList, vc.CurrentSystemOsID)
		s.OsID = listOsID
	})
	view, err := r.app.CompareOSDriverView(ctx, vc, listOsID)
	if err != nil {
		r.logf("ERROR", "驱动对比失败: "+err.Error())
		return
	}
	result := viewResult{
		MachineCaption: machineCaption(vc),
		OsList:         osEntries(vc.OsList, vc.CurrentSystemOsID),
		ListOsID:       listOsID,
		Rows:           projectRows(view.Selected),
	}
	r.notify(func(s *state) { s.applyExport(&result) })
	r.loadPlan()
	r.logf("INFO", "已加载 "+itoa(len(result.Rows))+" 行驱动列表，来自 "+osLabelFor(vc.OsList, listOsID))
}

// loadPlan reads the engine's plan artifact into the read-only plan pane. A
// missing plan (first run, failed comparison) leaves the pane's last content;
// the log already says the plan is not there.
func (r *engineRunner) loadPlan() {
	text, err := os.ReadFile(r.app.PlanPath)
	if err != nil {
		return
	}
	r.notify(func(s *state) {
		s.planEditor.Buffer.Reset(string(text))
	})
}

// resolve is the shared runtime resolution for every job: machine, OS list,
// local snapshots, then the OS to compare against.
func (r *engineRunner) resolve(ctx context.Context, osID string, includeBios, skipHashCheck bool) (*app.ViewContext, string, error) {
	opts := &app.Options{IncludeBios: includeBios, SkipHashCheck: skipHashCheck}
	vc, code := r.app.ResolveRuntime(ctx, opts)
	if code != 0 {
		r.logf("ERROR", "运行时解析失败，请查看日志。")
		return nil, "", errEngineFailed
	}
	listOsID := vc.CurrentSystemOsID
	if osID != "" {
		target := compare.ResolveTargetOsEntry(vc.OsList, osID)
		if target == nil {
			r.logf("ERROR", "无法从联想 OS 列表解析 OSID "+osID)
			return nil, "", errEngineFailed
		}
		listOsID = target.OSID
	}
	return vc, listOsID, nil
}

// Install downloads and installs the requested codes. req.codes comes from a
// confirmed dialog; the engine re-validates every code against the live view
// and refuses unknown or not-applicable ones.
func (r *engineRunner) Install(ctx context.Context, osID string, req installRequest, includeBios, skipHashCheck bool) {
	if !r.start(runInstall) {
		return
	}
	defer r.done()
	vc, listOsID, err := r.resolve(ctx, osID, includeBios, skipHashCheck)
	if err != nil {
		return
	}
	view, err := r.app.CompareOSDriverView(ctx, vc, listOsID)
	if err != nil {
		r.logf("ERROR", "驱动对比失败: "+err.Error())
		return
	}
	selected, code := r.app.SelectByCodes(vc, view, strings.Join(req.codes, ","))
	if code != 0 {
		r.logf("ERROR", "选中的驱动码与当前列表不匹配。")
		return
	}
	if req.downloadOnly {
		vc.Opts.DownloadOnly = true
	}
	if err := r.app.InstallSelected(ctx, vc, selected, view.Source, listOsID); err != nil {
		r.logf("ERROR", "安装失败: "+err.Error())
		return
	}
	if req.downloadOnly {
		r.logf("INFO", "驱动下载任务完成。")
		return
	}
	r.logf("INFO", "驱动下载/安装任务完成。")
	r.offerRollbacks(ctx)
}

// offerRollbacks surfaces pending rollback offers for informed consent. The
// ledger is the authority; the offer artifact supplies display detail.
func (r *engineRunner) offerRollbacks(ctx context.Context) {
	offers := r.app.PendingRollbackOffers()
	if len(offers) == 0 {
		return
	}
	r.notify(func(s *state) { openRollbackConfirm(s, offers) })
}

// openRollbackConfirm builds the rollback-consent dialog from the pending
// offers: one entry per pending offer with its device evidence, and a Confirm
// action that dispatches the rollback for the pending codes only.
func openRollbackConfirm(s *state, offers []app.PendingOffer) {
	var lines []string
	var codes []string
	for i := range offers {
		offer := &offers[i]
		if !offer.Pending() {
			continue
		}
		var devices []string
		for _, d := range offer.Devices {
			devices = append(devices, "    "+d.PnpDeviceID+" ("+d.Problem+")")
		}
		lines = append(lines,
			"  "+offer.DriverCode+"  "+offer.DriverName+"\n"+
				"    版本 "+offer.AfterVersion+" -> "+offer.BeforeVersion+"\n"+
				"    旧 INF "+offer.BeforeInf+"\n"+
				strings.Join(devices, "\n"))
		codes = append(codes, offer.DriverCode)
	}
	if len(codes) == 0 {
		return
	}
	message := "以下驱动安装后检测到设备问题，可回退到安装前的驱动程序：\n\n" +
		strings.Join(lines, "\n\n") + "\n\n" +
		"说明：问题码为本机实测；「由本次安装导致」为时间相关性推断，非直接测量。" +
		"回退会先回退驱动程序，无备份时重装旧 INF，不删除驱动包。是否执行？"
	s.Confirm = &confirmDialog{
		Title:   "回退确认",
		Message: message,
		Confirm: func() { s.dispatch(dispatchRequest{kind: runRollback, install: installRequest{codes: codes}}) },
	}
	s.confirming = true
}

// Rollback performs the pending rollback offers for the requested codes.
func (r *engineRunner) Rollback(ctx context.Context, codes []string) {
	if !r.start(runRollback) {
		return
	}
	defer r.done()
	if code := r.app.RunRollback(ctx, strings.Join(codes, ",")); code != 0 {
		r.logf("ERROR", "回退任务失败，请查看日志。")
		return
	}
	r.logf("INFO", "回退任务完成，请刷新列表查看结果。")
}

// OpenPlan opens the engine's plan file in the default editor. The plan path
// comes from the engine itself, never re-derived on the GUI side.
func (r *engineRunner) OpenPlan() {
	if _, err := os.Stat(r.app.PlanPath); err != nil {
		r.logf("INFO", "计划文件尚未生成。")
		return
	}
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", r.app.PlanPath).Start(); err != nil {
		r.logf("ERROR", "无法打开计划文件: "+err.Error())
	}
}

// projectRows renders the engine's assessed drivers as table rows.
func projectRows(assessed []*model.AssessedDriver) []driverRow {
	rows := make([]driverRow, 0, len(assessed))
	for _, ad := range assessed {
		if ad == nil || ad.Driver == nil {
			continue
		}
		sourceAudit := ""
		if ad.SourceAudit != nil {
			sourceAudit = string(ad.SourceAudit.Category) + ": " + ad.SourceAudit.Summary
		}
		rows = append(rows, driverRow{
			DriverCode:     ad.DriverCode,
			DriverName:     ad.DriverName,
			Version:        ad.Version,
			LocalVersion:   ad.LocalVersion,
			CompareStatus:  string(ad.CompareStatus),
			SourceAudit:    sourceAudit,
			EvidenceBasis:  model.StatusEvidenceBasis(ad.CompareStatus).String(),
			DeviceProblem:  ad.DeviceProblem,
			NonMatchReason: ad.NonMatchReason,
			FileName:       ad.FileName,
			FileSize:       ad.FileSize,
			// IsApplicable mirrors the CLI's automatic install set so the
			// GUI's install-all action offers the same drivers the CLI would.
			// A local definition here would re-admit Local newer.
			IsApplicable: model.InAutomaticInstallSet(ad.CompareStatus),
			IsUpdate:     ad.CompareStatus == model.StatusUpdate,
		})
	}
	return rows
}

// machineCaption renders the header caption.
func machineCaption(vc *app.ViewContext) string {
	return vc.Machine.Model + " / " + vc.Machine.Serial + "  |  当前系统 " + vc.OSInfo.Caption
}

// osEntries builds the dropdown list: the current system first, then the OS
// list in engine order.
func osEntries(osList []model.OSListEntry, currentID string) []osEntry {
	entries := make([]osEntry, 0, len(osList)+1)
	currentLabel := "当前系统"
	for _, entry := range osList {
		if entry.OSID == currentID {
			currentLabel = "当前系统 (" + entry.OSName + ")"
			break
		}
	}
	entries = append(entries, osEntry{Label: currentLabel, OSID: ""})
	for _, entry := range osList {
		entries = append(entries, osEntry{Label: entry.OSName + " (OSID " + entry.OSID + ")", OSID: entry.OSID})
	}
	return entries
}

func osLabelFor(osList []model.OSListEntry, osID string) string {
	for _, entry := range osList {
		if entry.OSID == osID {
			return entry.OSName
		}
	}
	return osID
}

// errEngineFailed marks a job whose engine step already logged its own error;
// the runner logs nothing twice.
var errEngineFailed = errJobFailed{}

type errJobFailed struct{}

func (errJobFailed) Error() string { return "engine job failed" }

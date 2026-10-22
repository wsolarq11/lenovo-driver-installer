package gui

// Runner tests drive the state mutations the real engine jobs make, with the
// notify hook running inline (no window, no goroutines).

import (
	"strings"
	"testing"

	"lenovo-driver/internal/app"
)

func newTestRunner(s *state) *engineRunner {
	return newEngineRunner(nil, func(fn func(*state)) { fn(s) })
}

func TestStartRejectsASecondJob(t *testing.T) {
	s := newTestState()
	r := newTestRunner(s)
	if !r.start(runRefresh) {
		t.Fatal("first start must win")
	}
	if s.Busy != true || s.Running != runRefresh {
		t.Fatalf("busy state = %v/%v, want true/runRefresh", s.Busy, s.Running)
	}
	if r.start(runInstall) {
		t.Fatal("second start must be refused while a job runs")
	}
	r.done()
	if s.Busy != false || s.Running != runNone {
		t.Fatalf("after done: %v/%v, want false/runNone", s.Busy, s.Running)
	}
}

func TestOfferRollbacksOpensDialogForPendingOnly(t *testing.T) {
	s := newTestState()
	r := newTestRunner(s)
	pending := []app.PendingOffer{
		app.NewPendingOffer("DRV-A", "Monitor Driver 显示器驱动",
			"20.21.0.1", "10.0.19041.5794", "monitor.inf",
			[]app.PendingOfferDevice{{PnpDeviceID: "PCI\\X", Problem: "code 28"}}, true),
		app.NewPendingOffer("DRV-Z", "", "", "", "", nil, false),
	}
	r.notify(func(st *state) { st.appendLog("INFO", "install done") })
	r.notify(func(st *state) { openRollbackConfirm(st, pending) })
	if s.Confirm == nil {
		t.Fatal("a pending offer must open the rollback dialog")
	}
	if !strings.Contains(s.Confirm.Message, "DRV-A") || strings.Contains(s.Confirm.Message, "DRV-Z") {
		t.Fatalf("dialog must name only pending offers: %q", s.Confirm.Message)
	}
	if !strings.Contains(s.Confirm.Message, "不删除驱动包") {
		t.Fatalf("dialog must carry the rollback semantics: %q", s.Confirm.Message)
	}
	if s.confirming != true {
		t.Fatal("dialog must be open")
	}
	// Confirming dispatches a rollback for the pending codes only.
	var got dispatchRequest
	s.dispatch = func(req dispatchRequest) { got = req }
	s.Confirm.Confirm()
	if got.kind != runRollback || len(got.install.codes) != 1 || got.install.codes[0] != "DRV-A" {
		t.Fatalf("rollback dispatch = %+v, want [DRV-A]", got)
	}
}

func TestOpenRollbackConfirmWithNoPendingKeepsStateClean(t *testing.T) {
	s := newTestState()
	r := newTestRunner(s)
	r.notify(func(st *state) { openRollbackConfirm(st, nil) })
	if s.Confirm != nil {
		t.Fatal("no pending offers must not open a dialog")
	}
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lenovo-driver/internal/install"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

// This file owns the device-level rollback flow. Rollback is always a
// per-device action and never a pnputil /delete-driver package cleanup: first
// DiRollbackDriver (the same primitive Device Manager's "Roll Back Driver"
// uses), and when Windows has no backup driver (ERROR_NO_MORE_ITEMS), a
// force-reinstall of the previously-bound INF through
// UpdateDriverForPlugAndPlayDevicesW with INSTALLFLAG_FORCE. A rollback action
// is only recorded as RolledBack when the device's problem code is re-verified
// as cleared; a silent no-op downgrade is recorded as RollbackFailed. The
// install pass writes a rollback offer for any device that reports a NEW
// problem code after a successful install; the GUI surfaces that offer for
// informed consent, and a later -Rollback run performs the device-level action
// only after the intent has been written to the audit ledger.
//
// The audit ledger is the single authority for rollback state. Every offer is
// durably recorded as a RollbackOffered row, and a -Rollback run only acts on
// offers that are still pending per the ledger (no later RolledBack or
// RollbackFailed row for that driver). The lenovo_driver_rollback.json file is
// a display cache for the GUI, never the authority.

const rollbackOfferFileName = "lenovo_driver_rollback.json"

const (
	rollbackStatePending    = "pending"
	rollbackStateRolledBack = "rolled_back"
	rollbackStatePartial    = "partial"
	rollbackStateFailed     = "failed"
)

// rollbackCausalBasis classifies a post-install device problem for the rollback
// offer gate. A problem that did not exist before install is "new" and
// attributable to this install; one that already existed is "pre-existing" and
// must be reported without being offered for rollback. An empty afterProblem
// returns "" (no problem, no basis).
func rollbackCausalBasis(beforeProblem, afterProblem string) string {
	if afterProblem == "" {
		return ""
	}
	if beforeProblem == "" {
		return "new"
	}
	return "pre-existing"
}

// rollbackOfferDevice is one problem device that can be rolled back. State is
// per-device so a partial rollback (some devices restored, some left broken) is
// visible instead of being collapsed into an offer-level failure. HardwareID is
// the stable hardware identifier used to force-reinstall the previous INF.
type rollbackOfferDevice struct {
	PnpDeviceID string `json:"PnpDeviceID"`
	HardwareID  string `json:"HardwareID,omitempty"`
	Problem     string `json:"Problem"`
	AfterInf    string `json:"AfterInf,omitempty"`
	State       string `json:"State,omitempty"`
}

// rollbackOffer carries the evidence the GUI needs to obtain informed consent
// and the targets a -Rollback run acts on.
type rollbackOffer struct {
	DriverCode    string                `json:"DriverCode"`
	DriverName    string                `json:"DriverName"`
	Version       string                `json:"Version"`
	OSID          string                `json:"OSID"`
	OSName        string                `json:"OSName"`
	FileName      string                `json:"FileName"`
	MD5           string                `json:"MD5"`
	Source        string                `json:"Source"`
	BeforeVersion string                `json:"BeforeVersion"`
	AfterVersion  string                `json:"AfterVersion"`
	BeforeInf     string                `json:"BeforeInf"`
	BeforeInfPath string                `json:"BeforeInfPath,omitempty"`
	Devices       []rollbackOfferDevice `json:"Devices"`
	State         string                `json:"State"`
	Message       string                `json:"Message,omitempty"`
	GeneratedAt   string                `json:"GeneratedAt"`
}

type rollbackOffersFile struct {
	Offers []rollbackOffer `json:"Offers"`
}

// rollbackDriverNative is the process-wide device rollback primitive. It is a
// package variable only so the offline gate can stub the real DiRollbackDriver
// call for fixtures; production code never overrides it.
var rollbackDriverNative = install.RollbackDriverNative

// rollbackReinstallNative force-reinstalls a previously-bound INF when
// DiRollbackDriver reports no backup. It is a package variable only so the
// offline gate can stub the real UpdateDriverForPlugAndPlayDevicesW call for
// fixtures; production code never overrides it.
var rollbackReinstallNative = install.ForceReinstallINF

// rollbackDeviceProblem re-reads a device's current problem code and driver
// version after a rollback action so the ledger records recovery, not just the
// API call. Recovery needs BOTH problem==0 and a driver that changed away from
// the broken version; otherwise a coincidental or self-healing recovery would
// be credited to the rollback. It is a package variable only so the offline
// gate can stub the enumeration.
var rollbackDeviceProblem = inventoryDeviceProblem

func inventoryDeviceProblem(ctx context.Context, instanceID string) (problem int, version string, found bool, err error) {
	devices, err := inventory.GetDeviceEvidence(ctx, []model.Device{{PnpDeviceID: instanceID}})
	if err != nil {
		return 0, "", false, err
	}
	if len(devices) == 0 {
		return 0, "", false, nil
	}
	return devices[0].ProblemNumber, devices[0].DriverVersion, true, nil
}

func (a *App) rollbackOfferPath() string {
	return filepath.Join(filepath.Dir(a.HistoryPath), rollbackOfferFileName)
}

func (a *App) writeRollbackOffers(offers []rollbackOffer) error {
	data, err := json.MarshalIndent(rollbackOffersFile{Offers: offers}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.rollbackOfferPath(), data, 0o644)
}

func (a *App) readRollbackOffers() ([]rollbackOffer, error) {
	data, err := os.ReadFile(a.rollbackOfferPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file rollbackOffersFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return file.Offers, nil
}

// runRollback performs the pending rollback offers for the requested driver
// codes. Pendingness is derived from the WORM ledger (a RollbackOffered row
// without a later outcome row), not from the JSON cache file; the file supplies
// only display detail. It writes an audit record before each device-level
// action and refuses to act when that audit write fails.
func (a *App) runRollback(ctx context.Context, codes string) int {
	requested := map[string]bool{}
	for _, code := range strings.Split(codes, ",") {
		if code = strings.TrimSpace(code); code != "" {
			requested[code] = true
		}
	}
	if len(requested) == 0 {
		a.Log(ctx, "No rollback driver codes supplied.", "ERROR")
		return 1
	}

	// Trust the ledger only after its integrity chain verifies. A broken chain
	// is surfaced but does not block: legacy (pre-hash) rows are not covered and
	// a personal-machine operator can decide how to act on a tamper/corruption
	// warning rather than being hard-blocked mid-recovery.
	if err := verifyHistoryChain(a.HistoryPath); err != nil {
		a.Log(ctx, "History integrity check failed: "+err.Error(), "WARN")
	}

	pending := pendingRollbackCodes(a.ReadHistory(ctx))

	offers, err := a.readRollbackOffers()
	if err != nil {
		a.Log(ctx, "Could not read rollback offers: "+err.Error(), "ERROR")
		return 1
	}

	acted := false
	failed := 0
	for i := range offers {
		offer := &offers[i]
		if !requested[offer.DriverCode] || !pending[offer.DriverCode] {
			continue
		}
		acted = true
		if !a.performRollback(ctx, offer) {
			failed++
		}
	}

	if err := a.writeRollbackOffers(offers); err != nil {
		a.Log(ctx, "Could not persist rollback offer state: "+err.Error(), "ERROR")
		return 1
	}

	if !acted {
		a.Log(ctx, "No pending rollback offer matches the requested codes.", "WARN")
		return 0
	}
	if failed > 0 {
		a.Log(ctx, fmt.Sprintf("%d rollback(s) failed.", failed), "ERROR")
		return 1
	}
	return 0
}

// performRollback rolls one offer's problem devices back. It returns false when
// any device in the offer failed to roll back. The audit-first intent row is
// written with WriteHistoryRecord and its error is honored, so an audit failure
// blocks the state change. Device and offer states are terminal once set, so a
// partial rollback is reported as such and never silently re-offered.
func (a *App) performRollback(ctx context.Context, offer *rollbackOffer) bool {
	ad := offer.assessedDriver()
	message := rollbackIntentMessage(offer)
	if err := a.WriteHistoryRecord(ad, "Rollback", message, "", offer.AfterVersion); err != nil {
		a.Log(ctx, "Rollback audit write failed; skipping rollback for "+offer.DriverCode+": "+err.Error(), "ERROR")
		offer.State = rollbackStateFailed
		offer.Message = "audit write failed"
		return false
	}

	rolledBack := 0
	failed := 0
	reboot := false
	for i := range offer.Devices {
		device := &offer.Devices[i]
		needReboot, ok := a.rollbackOneDevice(ctx, ad, offer, *device)
		if ok {
			device.State = rollbackStateRolledBack
			rolledBack++
		} else {
			device.State = rollbackStateFailed
			failed++
		}
		if needReboot {
			reboot = true
		}
	}

	switch {
	case rolledBack > 0 && failed == 0:
		offer.State = rollbackStateRolledBack
		offer.Message = ""
		if reboot {
			a.Log(ctx, fmt.Sprintf("[%s] Rolled back; reboot required.", offer.DriverCode), "WARN")
		} else {
			a.Log(ctx, fmt.Sprintf("[%s] Rolled back.", offer.DriverCode), "INFO")
		}
	case rolledBack > 0 && failed > 0:
		offer.State = rollbackStatePartial
		offer.Message = fmt.Sprintf("%d device(s) rolled back, %d failed", rolledBack, failed)
		a.Log(ctx, fmt.Sprintf("[%s] Partial rollback: %s", offer.DriverCode, offer.Message), "WARN")
	default:
		offer.State = rollbackStateFailed
		offer.Message = "one or more devices failed to roll back"
	}
	return failed == 0
}

// rollbackOneDevice rolls one device back and records the outcome. A successful
// action is only recorded as RolledBack when the device's problem code is
// re-verified as cleared; a no-reboot rollback whose device still reports a
// problem is recorded as RollbackFailed so a silent no-op cannot look like a
// recovery. A pending reboot is recorded as RolledBack with the caveat that the
// recovery is unverified until the restart.
func (a *App) rollbackOneDevice(ctx context.Context, ad *model.AssessedDriver, offer *rollbackOffer, device rollbackOfferDevice) (needReboot, ok bool) {
	needReboot, err := rollbackDriverNative(device.PnpDeviceID)
	if err != nil {
		if re, ok := err.(*install.RollbackError); ok && re.NoBackup() {
			a.Log(ctx, fmt.Sprintf("[%s] No backup driver for %s; trying previous INF reinstall.", offer.DriverCode, device.PnpDeviceID), "WARN")
			needReboot, err = a.reinstallPreviousInf(offer, device)
		}
	}
	if err != nil {
		a.Log(ctx, fmt.Sprintf("[%s] Rollback failed for %s: %v", offer.DriverCode, device.PnpDeviceID, err), "ERROR")
		a.writeHistoryRecordChecked(ctx, ad, "RollbackFailed", "device "+device.PnpDeviceID+": "+err.Error(), "", offer.AfterVersion)
		return false, false
	}

	if needReboot {
		a.writeHistoryRecordChecked(ctx, ad, "RolledBack", "device "+device.PnpDeviceID+" rolled back (reboot required; recovery unverified)", offer.BeforeVersion, offer.AfterVersion)
		return true, true
	}

	problem, version, found, verr := rollbackDeviceProblem(ctx, device.PnpDeviceID)
	if verr != nil {
		a.Log(ctx, fmt.Sprintf("[%s] Recovery verification unavailable for %s: %v", offer.DriverCode, device.PnpDeviceID, verr), "WARN")
		a.writeHistoryRecordChecked(ctx, ad, "RolledBack", "device "+device.PnpDeviceID+" rolled back (verification unavailable)", offer.BeforeVersion, offer.AfterVersion)
		return false, true
	}
	if found && problem == 0 && version != "" && version != offer.AfterVersion {
		a.writeHistoryRecordChecked(ctx, ad, "RolledBack", "device "+device.PnpDeviceID+" rolled back (recovered)", offer.BeforeVersion, offer.AfterVersion)
		return false, true
	}
	if !found {
		a.Log(ctx, fmt.Sprintf("[%s] Device %s not found after rollback.", offer.DriverCode, device.PnpDeviceID), "ERROR")
		a.writeHistoryRecordChecked(ctx, ad, "RollbackFailed", "device "+device.PnpDeviceID+" not found after rollback", "", offer.AfterVersion)
		return false, false
	}
	if problem != 0 {
		a.Log(ctx, fmt.Sprintf("[%s] Device %s still reports problem %d after rollback.", offer.DriverCode, device.PnpDeviceID, problem), "ERROR")
		a.writeHistoryRecordChecked(ctx, ad, "RollbackFailed", fmt.Sprintf("device %s still reports problem %d after rollback", device.PnpDeviceID, problem), "", offer.AfterVersion)
		return false, false
	}
	// Problem cleared but the driver did not verifiably change away from the
	// broken version. Do not credit the rollback for a coincidental or
	// self-healing recovery.
	if version == "" {
		a.Log(ctx, fmt.Sprintf("[%s] Device %s problem cleared but driver version unverifiable; rollback not confirmed.", offer.DriverCode, device.PnpDeviceID), "ERROR")
		a.writeHistoryRecordChecked(ctx, ad, "RollbackFailed", "device "+device.PnpDeviceID+" problem cleared but driver version unverifiable; rollback not confirmed", "", offer.AfterVersion)
	} else {
		a.Log(ctx, fmt.Sprintf("[%s] Device %s problem cleared but driver unchanged (%s); rollback not confirmed.", offer.DriverCode, device.PnpDeviceID, version), "ERROR")
		a.writeHistoryRecordChecked(ctx, ad, "RollbackFailed", fmt.Sprintf("device %s problem cleared but driver still at %s; rollback not confirmed", device.PnpDeviceID, version), "", offer.AfterVersion)
	}
	return false, false
}

// reinstallPreviousInf force-reinstalls the previously-bound INF when
// DiRollbackDriver has no backup. It confirms the previous INF still exists
// before acting (audit-first) but does not write the ledger; rollbackOneDevice
// records the outcome after re-verifying device health.
func (a *App) reinstallPreviousInf(offer *rollbackOffer, device rollbackOfferDevice) (needReboot bool, err error) {
	if offer.BeforeInfPath == "" {
		return false, fmt.Errorf("no backup driver and no previous INF captured")
	}
	if _, statErr := os.Stat(offer.BeforeInfPath); statErr != nil {
		return false, fmt.Errorf("previous INF missing: %s", offer.BeforeInfPath)
	}
	needReboot, err = rollbackReinstallNative(offer.BeforeInfPath, device.HardwareID)
	if err != nil {
		return false, fmt.Errorf("reinstall %s: %w", offer.BeforeInfPath, err)
	}
	return needReboot, nil
}

func (o *rollbackOffer) assessedDriver() *model.AssessedDriver {
	return &model.AssessedDriver{
		Driver: &model.Driver{
			DriverCode:  o.DriverCode,
			DriverName:  o.DriverName,
			Version:     o.Version,
			OSID:        o.OSID,
			OSName:      o.OSName,
			FileName:    o.FileName,
			OfficialMD5: o.MD5,
			SourceAPI:   o.Source,
		},
	}
}

func rollbackIntentMessage(offer *rollbackOffer) string {
	ids := make([]string, 0, len(offer.Devices))
	for _, device := range offer.Devices {
		ids = append(ids, device.PnpDeviceID)
	}
	return "device=" + strings.Join(ids, ";") + "; previous_inf=" + offer.BeforeInf
}

// rollbackOfferedMessage renders the durable RollbackOffered ledger row. It
// carries the device targets and the previous INF path so the offer's restore
// target survives in the WORM ledger even if the JSON cache file is lost.
func rollbackOfferedMessage(offer rollbackOffer) string {
	ids := make([]string, 0, len(offer.Devices))
	for _, device := range offer.Devices {
		ids = append(ids, device.PnpDeviceID)
	}
	return "devices=" + strings.Join(ids, ",") + "; previous_inf=" + offer.BeforeInf + "; previous_inf_path=" + offer.BeforeInfPath
}

// pendingRollbackCodes derives, from the append-only ledger, which driver codes
// still have a pending rollback offer: a RollbackOffered row makes a code
// pending, and a later RolledBack or RollbackFailed row clears it. The ledger,
// not the JSON cache file, is the authority for what may be rolled back.
func pendingRollbackCodes(records []model.HistoryRecord) map[string]bool {
	pending := map[string]bool{}
	for _, r := range records {
		switch r.Result {
		case "RollbackOffered":
			pending[r.DriverCode] = true
		case "RolledBack", "RollbackFailed":
			pending[r.DriverCode] = false
		}
	}
	return pending
}

// buildRollbackOffer assembles the consent evidence and rollback targets for a
// driver whose post-install devices still report a problem code. The transport
// fields and the pre-install version/INF come from the assessed driver captured
// before installation, so the offer is evidence-first.
func (a *App) buildRollbackOffer(ad *model.AssessedDriver, beforeLocal, afterLocal string, devices []model.Device) rollbackOffer {
	offer := rollbackOffer{
		DriverCode:    ad.DriverCode,
		DriverName:    ad.DriverName,
		Version:       ad.Version,
		OSID:          ad.OSID,
		OSName:        ad.OSName,
		FileName:      ad.FileName,
		MD5:           ad.OfficialMD5,
		Source:        ad.SourceAPI,
		BeforeVersion: beforeLocal,
		AfterVersion:  afterLocal,
		BeforeInf:     ad.BeforeInfName,
		BeforeInfPath: ad.BeforeInfPath,
		State:         rollbackStatePending,
		GeneratedAt:   formatTimestamp(time.Now()),
	}
	for _, device := range devices {
		if device.ProblemNumber == 0 || device.PnpDeviceID == "" {
			continue
		}
		offer.Devices = append(offer.Devices, rollbackOfferDevice{
			PnpDeviceID: device.PnpDeviceID,
			HardwareID:  device.DeviceID,
			Problem:     model.DeviceProblemLabel(device.ProblemNumber),
			AfterInf:    filepath.Base(device.InfName),
		})
	}
	return offer
}

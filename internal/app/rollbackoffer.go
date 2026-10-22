package app

// The rollback offer artifact: its shape, its state vocabulary, and the JSON
// cache the GUI reads. The offer is display data; the WORM ledger remains the
// authority for whether a rollback is still pending.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"lenovo-driver/internal/install"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

const rollbackOfferFileName = "lenovo_driver_rollback.json"

const (
	rollbackStatePending    = "pending"
	rollbackStateRolledBack = "rolled_back"
	rollbackStatePartial    = "partial"
	rollbackStateFailed     = "failed"
)

// causalBasis says whether a post-install device problem can be attributed to
// the install that just ran. It was a string where the empty value meant "no
// problem", a fourth state sharing a spelling with "unset". The zero value is
// now None, so a forgotten assignment cannot read as an attribution.
type causalBasis int

const (
	// causalNone is the ZERO VALUE: the device reports no problem, so there is
	// no causation question to answer.
	causalNone causalBasis = iota
	// causalNew means the problem did not exist before this install, so it is
	// attributable to it and may be offered for rollback.
	causalNew
	// causalPreExisting means the problem already existed, so it must be
	// reported without being attributed or offered.
	causalPreExisting
)

func (b causalBasis) String() string {
	switch b {
	case causalNew:
		return "new"
	case causalPreExisting:
		return "pre-existing"
	default:
		return ""
	}
}

// rollbackCausalBasis classifies a post-install device problem for the rollback
// offer gate. A problem that did not exist before install is attributable to
// this install; one that already existed must be reported without being offered.
func rollbackCausalBasis(beforeProblem, afterProblem string) causalBasis {
	if afterProblem == "" {
		return causalNone
	}
	if beforeProblem == "" {
		return causalNew
	}
	return causalPreExisting
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

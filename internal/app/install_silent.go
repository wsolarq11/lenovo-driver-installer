package app

import (
	"path/filepath"
	"strings"

	"lenovo-driver/internal/install"
	"lenovo-driver/internal/model"
)

// silentInstallEvidence resolves which mechanism authorises an unattended
// install of outFile, and whether the package can run without an operator.
//
// The evidence is prose for the audit ledger: it names the installer family. A
// run that installs with nobody present must be able to answer afterwards why
// the tool believed it could, because a wrong family and a right one both
// return exit code 0 and the same silent outcome. The evidence must never land
// in a version column — that would turn "Inno Setup marker" into a version
// claim. interactive is true when the package has no proven silent mechanism.
func silentInstallEvidence(driver *model.Driver, outFile string) (evidence string, interactive bool) {
	if !strings.EqualFold(filepath.Ext(outFile), ".exe") {
		// INF payloads need no silent switch; handing the INF to the driver
		// store is already unattended.
		return "", false
	}
	canRun, evidence := install.SilentPlanFor(outFile, driver)
	if !canRun {
		return evidence, true
	}
	return evidence, false
}

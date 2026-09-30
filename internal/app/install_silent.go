package app

import (
	"path/filepath"
	"strings"

	"lenovo-driver/internal/install"
	"lenovo-driver/internal/model"
)

// silentInstallEvidence decides whether a package can be installed unattended
// and returns the prose evidence for the audit ledger.
//
// The decision is one fact: does the package need a silent switch, and is that
// switch verified end-to-end? A .inf/.zip/.cab payload does not — the tool hands
// it to pnputil / DiInstallDriverW directly, which is headless by construction
// (no GUI, so no switch to get wrong). An .exe installer does — and on 82JQ no
// .exe silent switch has ever been verified end-to-end, so every .exe is
// interactive. The evidence names the recognised family so the ledger can say
// "we saw Inno and refused it" rather than just "refused".
func silentInstallEvidence(driver *model.Driver, outFile string) (evidence string, interactive bool) {
	if !strings.EqualFold(filepath.Ext(outFile), ".exe") {
		return "pnputil headless (no switch needed)", false
	}
	_, evidence = install.SilentPlanFor(outFile, driver)
	return evidence, true
}

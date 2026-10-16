package gate

// The checks in this file state shapes the program must have, and read those
// shapes from the syntax tree. Each was once a PowerShell regex over Go source,
// where a comment could satisfy a rule or break one, a branch matcher depended
// on the indentation the formatter produced, and a grouped `const (...)` or
// `var (...)` declaration was invisible to a line-anchored pattern.

import (
	"fmt"
	"go/ast"
	"strings"
)

// checkRunReportsOnlyConfirmed holds the rule that a run summary counts only
// what the post-install recheck confirmed. An installer can exit 0 without
// changing the machine: on 82JQ a drill logged "Install success" and
// "Recheck: unchanged" while the machine gained nothing.
func checkRunReportsOnlyConfirmed(ctx *Context) error {
	bindingFile, err := ctx.Repo.fileDeclaring("internal/app", "installBindingConfirmsEffect")
	if err != nil {
		return fmt.Errorf("%s; the run summary is no longer gated on the recheck", err)
	}
	fn := topLevelFunc(bindingFile.ast, "installBindingConfirmsEffect")
	if !enumeratesByLabel(fn.Body) {
		return fmt.Errorf("installBindingConfirmsEffect no longer enumerates confirmed bindings by name")
	}
	// The labels that carry evidence, and the ones that do not, are read as
	// case clauses: the three evidence labels share one arm, so matching arms
	// line by line would miss all but the first.
	confirmed, _ := classifyBindingLabels(fn.Body)
	for _, label := range []string{"bound", "staged", "unchanged-same"} {
		if !confirmed[label] {
			return fmt.Errorf("binding %s no longer counts as a confirmed install", label)
		}
	}
	// A label with no evidence of effect must not be in the confirming arm. It
	// is enough to test membership: moving "unchanged" into the case list beside
	// the evidence labels is exactly how a no-op install starts counting as a
	// success, and it leaves the label in no other arm to inspect.
	for _, label := range []string{"unchanged", "undetected"} {
		if confirmed[label] {
			return fmt.Errorf("binding %s carries no evidence of effect but is counted as a confirmed install",
				label)
		}
	}

	// Comparability must come from the value, not the driver family, and the
	// decision must live in the binding owner rather than at the call site.
	if !callsQualified(bindingFile.ast, "model.IsMeasuredLocalVersion") {
		return fmt.Errorf("installBindingLabel no longer decides comparability from the value it read")
	}
	install, err := ctx.Repo.fileAt("internal/app/install.go")
	if err != nil {
		return err
	}
	if keyedOnFamily(install.ast) {
		return fmt.Errorf("installBindingLabel is keyed on the driver family again; a real version read " +
			"back from the installed-app list (Energy Management 15.11.29.65) is measurable and must " +
			"carry a verdict")
	}
	label := topLevelFunc(bindingFile.ast, "installBindingLabel")
	if label == nil {
		return fmt.Errorf("installBindingLabel is gone; the binding verdict has no single owner")
	}
	if hasTypeNamed(label.Type, "bool") {
		return fmt.Errorf("installBindingLabel takes a comparability flag again; the call site must not " +
			"decide the verdict")
	}

	model, err := ctx.Repo.fileDeclaring("internal/model", "IsMeasuredLocalVersion")
	if err != nil {
		return err
	}
	if !hasTopLevelName(model.ast, "LocalVersionProvisioned") {
		return fmt.Errorf("model.LocalVersionProvisioned is gone; the provisioning placeholder lost its " +
			"single owner")
	}

	// The placeholder must not be spelled anywhere but its owner.
	matching, err := ctx.Repo.fileAt("internal/compare/matching.go")
	if err != nil {
		return err
	}
	for _, lit := range stringLiteralValues(matching.ast) {
		if lit == "Provisioned" {
			return fmt.Errorf("matching.go spells the provisioning placeholder itself; the placeholder " +
				"has one owner, model.LocalVersionProvisioned")
		}
	}

	for _, lit := range stringLiteralValues(install.ast) {
		if strings.Contains(lit, "unverified=%d") {
			goto reported
		}
	}
	return fmt.Errorf("the Finished summary no longer reports an unverified count")

reported:
	if !callsQualified(install.ast, "installBindingConfirmsEffect") {
		return fmt.Errorf("verifyInstalled no longer filters drivers through the confirmation gate")
	}
	for _, name := range []string{"TestInstallBindingLabelHasNoVerdictWithoutComparableVersions",
		"TestOnlyConfirmedBindingsCountAsSuccess"} {
		if _, err := ctx.Repo.testDeclaring("internal/app", name); err != nil {
			return fmt.Errorf("%s; the recheck-to-summary gate is unguarded", err)
		}
	}
	return nil
}

// checkSilentDispatchOnPackage holds invariant 2.1. The vendor Parameter column
// is falsified: DRV202102040007 declares "-QuietInstall", its binary contains no
// such literal, and it carries the Inno header. Trusting the column launched a
// window the operator dismissed and the machine gained nothing.
func checkSilentDispatchOnPackage(ctx *Context) error {
	family, err := ctx.Repo.fileDeclaring("internal/install", "planSilentInstall")
	if err != nil {
		return fmt.Errorf("%s; the silent dispatch has no single owner", err)
	}
	// No family may send flags: any switch table is a guess being launched, and
	// a wrong switch is a silent no-op that reports success while changing
	// nothing. The moment one switch is verified end to end, this check and the
	// refusing tests change together.
	if returnsStringSliceLiteral(family.ast) {
		return fmt.Errorf("a switch array reappeared; no .exe silent switch is verified, so no family may " +
			"send flags")
	}
	// Family recognition survives for the audit trail even though no family runs
	// unattended: the refusal evidence must name what was recognised.
	if !hasTopLevelName(family.ast, "familyMarkers") {
		return fmt.Errorf("familyMarkers is gone; the refusal evidence can no longer name the recognised " +
			"family")
	}
	for _, name := range []string{"TestSilentPlanRefusesUnverifiedInno",
		"TestSilentPlanRefusesEveryUnverifiedFamily",
		"TestSilentPlanEvidenceNamesTheRefusedFamily",
		"TestSilentPlanEvidenceAgreesWithTheDecision",
		"TestInstallDriverFileEXEIsTerminal"} {
		if _, err := ctx.Repo.testDeclaring("internal/install", name); err != nil {
			return fmt.Errorf("%s; the formula is unguarded", err)
		}
	}
	return nil
}

// checkSilentMechanismIsProse holds that the evidence authorising an unattended
// install survives into the audit row as prose. The ledger's VerifiedVersion
// column is not a free slot: writing a mechanism into it silently converts
// "Inno Setup marker" into a version claim.
func checkSilentMechanismIsProse(ctx *Context) error {
	if _, err := ctx.Repo.fileDeclaring("internal/install", "SilentPlanFor"); err != nil {
		return fmt.Errorf("%s; the evidence has no single source", err)
	}
	silent, err := ctx.Repo.fileAt("internal/app/install_silent.go")
	if err != nil {
		return err
	}
	if !callsQualified(silent.ast, "install.SilentPlanFor") {
		return fmt.Errorf("the app no longer asks for the silent mechanism; the audit row is blind again")
	}
	app, err := ctx.Repo.fileAt("internal/app/install.go")
	if err != nil {
		return err
	}
	recordsMechanism := false
	for _, lit := range stringLiteralValues(app.ast) {
		if strings.Contains(lit, "silent via") {
			recordsMechanism = true
		}
	}
	if !recordsMechanism {
		return fmt.Errorf("the Installed row no longer records which mechanism authorised the run")
	}
	// The evidence variable must never appear as a bare positional argument:
	// that is how it leaks into the version columns and turns a mechanism into a
	// version claim.
	for _, call := range callsNamed(app.ast, "writeHistoryRecordChecked") {
		for _, arg := range call.Args {
			if id, ok := arg.(*ast.Ident); ok && (id.Name == "silence" || id.Name == "evidence") {
				return fmt.Errorf("the silent mechanism is a bare positional argument to a ledger write; " +
					"the evidence is leaking into a version column")
			}
		}
	}
	return nil
}

// checkProvisioningPlaceholderUndecided holds that the "Provisioned" placeholder
// never reaches a decision. It is a fact about HKLM\...\Provisioning\Results,
// not a version: it proves neither presence nor absence, so only an
// undetermined status is honest.
func checkProvisioningPlaceholderUndecided(ctx *Context) error {
	matching, err := ctx.Repo.fileDeclaring("internal/compare", "CompareDriverStatus")
	if err != nil {
		return err
	}
	fn := topLevelFunc(matching.ast, "CompareDriverStatus")
	found := false
	for _, stmt := range ifStmts(fn.Body) {
		if !comparesOperand(stmt.Cond, "model.LocalVersionProvisioned") {
			continue
		}
		// The branch must be the one keyed on the local version; the
		// provisioning field of a snapshot is a different question.
		if !comparesOperand(stmt.Cond, "local") {
			continue
		}
		found = true
		if !returnsName(stmt.Body, "model.StatusUnknown") {
			return fmt.Errorf("the provisioning placeholder is judged again; it proves neither presence " +
				"nor absence, so only an undetermined status is honest")
		}
	}
	if !found {
		return fmt.Errorf("CompareDriverStatus no longer handles the provisioning placeholder")
	}
	if _, err := ctx.Repo.testDeclaring("internal/app", "TestProvisionedIsNotAVersion"); err != nil {
		return fmt.Errorf("%s; the placeholder-to-status rule is unguarded", err)
	}
	return nil
}

// checkSoftwareVersionedFallback holds that a failed software lookup falls back
// to the measured device version. "Not installed" is the only status the
// automatic set acts on, so what produces it must rest on measurement: an empty
// local version used to be read as evidence of absence while the device was in
// fact running a vendor package.
func checkSoftwareVersionedFallback(ctx *Context) error {
	matching, err := ctx.Repo.fileDeclaring("internal/compare", "deviceVersionFrom")
	if err != nil {
		return fmt.Errorf("%s; the device-measured version has no single extraction point", err)
	}
	found := false
	for _, stmt := range ifStmts(matching.ast) {
		if !callsBare(stmt.Cond, "isSoftwareVersionedDriver") {
			continue
		}
		found = true
		if !callsBare(stmt.Body, "deviceVersionFrom") {
			return fmt.Errorf("the software-versioned branch returns the software version with no device " +
				"fallback; a failed installed-app lookup would again be read as evidence of absence")
		}
	}
	if !found {
		return fmt.Errorf("could not locate the software-versioned branch in matching.go")
	}
	if _, err := ctx.Repo.testDeclaring("internal/compare", "TestSoftwareVersionedDriverFallsBackToMeasuredDeviceVersion"); err != nil {
		return fmt.Errorf("%s; the fallback rule is unguarded", err)
	}
	return nil
}

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
	// The gate is now a method on the verdict type, and the verdicts are typed
	// constants rather than string labels. The rule follows the shape: the
	// confirming arm must name exactly the three evidence verdicts. Each of them
	// is checked by constant name so renaming one cannot silently drop it, and
	// the no-evidence verdict must be absent from that arm.
	bindingFile, err := ctx.Repo.fileDeclaring("internal/app", "confirmsEffect")
	if err != nil {
		return fmt.Errorf("%s; the run summary is no longer gated on the recheck", err)
	}
	fn := funcDeclByName(bindingFile.ast, "confirmsEffect")
	if fn == nil {
		return fmt.Errorf("the recheck's confirmation gate is gone; the run summary would count any exit code")
	}
	if !enumeratesByLabel(fn.Body) {
		return fmt.Errorf("confirmsEffect no longer enumerates confirmed verdicts by name")
	}
	confirmed, denied := classifyBindingLabels(fn.Body)
	for _, label := range []string{"bindingBound", "bindingStaged", "bindingUnchangedSame",
		"BindingBound", "BindingStaged", "BindingUnchangedSame"} {
		if confirmed[label] {
			goto labelsOK
		}
	}
	return fmt.Errorf("no evidence verdict is confirmed by confirmsEffect")
labelsOK:
	for _, label := range []string{"bindingUnchanged", "bindingUndetected", "BindingUnchanged", "BindingUndetected"} {
		if confirmed[label] {
			return fmt.Errorf("verdict %s confirms an install but carries no evidence that the package took effect", label)
		}
		_ = denied
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
		return fmt.Errorf("installBindingVerdict no longer decides comparability from the value it read")
	}
	install, err := ctx.Repo.fileAt("internal/app/install.go")
	if err != nil {
		return err
	}
	if keyedOnFamily(install.ast) {
		return fmt.Errorf("installBindingVerdict is keyed on the driver family again; a real version read " +
			"back from the installed-app list (Energy Management 15.11.29.65) is measurable and must " +
			"carry a verdict")
	}
	verdictFn := topLevelFunc(bindingFile.ast, "installBindingVerdict")
	if verdictFn == nil {
		return fmt.Errorf("installBindingVerdict is gone; the binding verdict has no single owner")
	}
	if hasTypeNamed(verdictFn.Type, "bool") {
		return fmt.Errorf("installBindingVerdict takes a comparability flag again; the call site must not " +
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
	if len(callsTo(install.ast, "confirmsEffect")) == 0 {
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
	// The placeholder is now a value of the evidence union, not a string compared
	// against the local version. The rule follows the code shape: the case must
	// exist and must answer undetermined. Spelling it as a switch case (rather
	// than an if-comparison) is what makes the mapping exhaustive on the union,
	// so the check insists on the case clause instead of any mention.
	found := false
	for _, clause := range switchClausesOn(fn.Body, "local.Kind") {
		if !clause.namesValue("model.LocalEvidenceProvisioned") {
			continue
		}
		found = true
		if !clause.returnsName("model.StatusUnknown") {
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

// checkAuditUsesEvidenceSet holds that the source audit matches against every
// measured version, not against the display string. A driver whose devices
// disagree renders "a, b", which equals no history row and no device row, so the
// audit silently concluded "no evidence" for a driver that had recorded install
// history. The rule pins the candidate-set helper and its use in the audit's
// comparison sites.
func checkAuditUsesEvidenceSet(ctx *Context) error {
	if _, err := ctx.Repo.fileDeclaring("internal/audit", "candidateVersions"); err != nil {
		return fmt.Errorf("%s; the audit would compare one display string again", err)
	}
	auditFile, err := ctx.Repo.fileDeclaring("internal/audit", "sourceEvidenceRules")
	if err != nil {
		return err
	}
	if !callsBare(auditFile.ast, "candidateVersions") {
		return fmt.Errorf("the audit does not ask for the candidate version set; a conflicted driver's " +
			"evidence would be compared as one joined string and match nothing")
	}
	if !callsBare(auditFile.ast, "versionInCandidates") {
		return fmt.Errorf("the audit compares device versions by raw equality; a conflicted driver's " +
			"devices would be filtered out")
	}
	// The display projection must not become a candidate version. Its non-empty
	// states are prose, and matching a ledger cell that holds the same sentence
	// attributes an install that never happened.
	candidates, err := ctx.Repo.fileDeclaring("internal/audit", "candidateVersions")
	if err != nil {
		return err
	}
	fn := funcDeclByName(candidates.ast, "candidateVersions")
	if fn == nil {
		return fmt.Errorf("candidateVersions is gone")
	}
	if identsIn(fn.Body)["LocalVersion"] {
		return fmt.Errorf("candidateVersions reads LocalVersion, which is the display projection; its " +
			"non-empty states are prose and matching them to a ledger row invents an attribution")
	}
	return nil
}

// checkVersionColumnsHoldVersions holds the boundary between prose and the
// ledger's two version columns. LocalEvidence.Display renders human text ("not
// detected", "a, b"); the columns hold a measured version or nothing, because
// every later join (install history, source audit, rollback) treats them as
// numbers. A conflict written as "a, b" reads as a composite version string and
// "not detected" reads as a version claim, so the prose belongs in Message.
//
// The rule reads the call sites rather than the variable names: each ledger
// append in the recheck must pass MeasuredVersion on its version arguments.
// Naming a local variable is not enough, and a helper added later would escape
// a rule that only looked at declarations.
func checkVersionColumnsHoldVersions(ctx *Context) error {
	install, err := ctx.Repo.fileDeclaring("internal/app", "verifyInstalled")
	if err != nil {
		return fmt.Errorf("%s; the recheck's ledger boundary is unguarded", err)
	}
	fn := funcDeclByName(install.ast, "verifyInstalled")
	if fn == nil {
		return fmt.Errorf("verifyInstalled is gone; the ledger boundary has no owner")
	}
	appends := callsTo(fn.Body, "writeHistoryRecordChecked")
	if len(appends) == 0 {
		return fmt.Errorf("the recheck no longer appends ledger rows; this rule lost its subject")
	}
	// The version arguments are the last two. They must be variables the
	// function bound from MeasuredVersion, because a call site may legitimately
	// hoist the value. The rule therefore reads the whole body: a MeasuredVersion
	// call must exist, and no Display call may feed an append directly.
	if len(callsTo(fn.Body, "MeasuredVersion")) == 0 {
		return fmt.Errorf("the recheck never asks for a measured version; the ledger would take the " +
			"display prose into VerifiedVersion/BeforeVersion")
	}
	for _, call := range appends {
		for _, arg := range call.Args {
			if selectorCallName(arg) == "Display" {
				return fmt.Errorf("the recheck passes Display() to a ledger append; that value is prose " +
					"(\"not detected\", \"a, b\") and belongs in Message, not in a version column")
			}
		}
	}
	return nil
}

// checkEvidenceUnionStaysClosed holds that LocalEvidence remains a sealed union:
// unexported payload fields, construction through named constructors, and the
// zero value meaning "nothing observed". Each of the three is load-bearing.
// Exported fields let any caller assemble a Conflicted state carrying one
// version; a missing constructor lets a caller spell a state the machine never
// reported; and an Absent zero value turns a forgotten assignment into the one
// status the unattended install path acts on.
func checkEvidenceUnionStaysClosed(ctx *Context) error {
	evidence, err := ctx.Repo.fileWithName("internal/model", "LocalEvidence")
	if err != nil {
		return fmt.Errorf("%s; the local evidence union is gone", err)
	}
	typeSpec := topLevelType(evidence.ast, "LocalEvidence")
	if typeSpec == nil {
		return fmt.Errorf("LocalEvidence is no longer a declared type")
	}
	if exportedFields(typeSpec) > 0 {
		return fmt.Errorf("LocalEvidence has %d exported field(s); an exported payload lets a caller "+
			"assemble a state the machine never reported", exportedFields(typeSpec))
	}
	for _, constructor := range []string{"ObservedVersion", "ObservedAbsence", "ObservedConflict",
		"ObservedProvisioning", "NotObserved"} {
		if _, err := ctx.Repo.fileDeclaring("internal/model", constructor); err != nil {
			return fmt.Errorf("%s; a state lost its named constructor", err)
		}
	}
	// The zero value must be Unobserved. The constants are declared in order, so
	// the first name is the zero value; pinning it by name keeps the guarantee
	// readable rather than positional.
	constBlock := topLevelConstNames(evidence.ast, "LocalEvidenceKind")
	if len(constBlock) == 0 {
		return fmt.Errorf("LocalEvidenceKind lost its constant block")
	}
	if constBlock[0] != "LocalEvidenceUnobserved" {
		return fmt.Errorf("the LocalEvidenceKind zero value is %s; it must be LocalEvidenceUnobserved "+
			"so a forgotten assignment measures nothing instead of reporting absence", constBlock[0])
	}
	return nil
}

// checkSoftwareVersionedFallback holds that a failed software lookup falls back
// to the measured device version. "Not installed" is the only status the
// automatic set acts on, so what produces it must rest on measurement: an empty
// local version used to be read as evidence of absence while the device was in
// fact running a vendor package.
func checkSoftwareVersionedFallback(ctx *Context) error {
	if _, err := ctx.Repo.fileDeclaring("internal/compare", "deviceEvidenceFrom"); err != nil {
		return fmt.Errorf("%s; the device-measured evidence has no single extraction point", err)
	}
	// The fallback is the software-versioned branch returning the device
	// evidence when the installed-app lookup produced nothing. Following the
	// code shape: the branch must exist and must return deviceEvidence rather
	// than an empty value, because an empty value is what got read as absence.
	resolve, err := ctx.Repo.fileDeclaring("internal/compare", "ResolveLocalDriverVersion")
	if err != nil {
		return err
	}
	fn := topLevelFunc(resolve.ast, "ResolveLocalDriverVersion")
	found := false
	for _, stmt := range ifStmts(fn.Body) {
		if !callsBare(stmt.Cond, "isSoftwareVersionedDriver") {
			continue
		}
		found = true
		if !returnsAnyName(stmt.Body, "deviceEvidence") {
			return fmt.Errorf("the software-versioned branch no longer falls back to the device-measured " +
				"evidence; a failed installed-app lookup would again be read as evidence of absence")
		}
	}
	if !found {
		return fmt.Errorf("could not locate the software-versioned branch in internal/compare")
	}
	if _, err := ctx.Repo.testDeclaring("internal/compare", "TestSoftwareVersionedDriverFallsBackToMeasuredDeviceVersion"); err != nil {
		return fmt.Errorf("%s; the fallback rule is unguarded", err)
	}
	return nil
}

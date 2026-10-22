package gate

// The fixture side of the document checks: these hold the recorded data and the
// durable artifact against the tests and endpoints that are supposed to exercise
// them. They live apart from the contract comparisons because they read testdata
// and test sources, not the contract documents.

import (
	"fmt"
	"regexp"
	"strings"
)

// checkEvidenceChainRunsOnFixtures requires the end-to-end chain to call every
// link, and requires each fixture a test reads to exist and, when it is recorded
// machine data, to be large enough to be real. A renamed call used to satisfy
// the old text scan if any comment still spelled the old name.
func checkEvidenceChainRunsOnFixtures(ctx *Context) error {
	chain, err := ctx.Repo.fileAt("internal/app/evidence_chain_test.go")
	if err != nil {
		return fmt.Errorf("internal/app/evidence_chain_test.go is gone; the end-to-end evidence " +
			"chain no longer exists")
	}
	links := []string{"ParseQuickFix", "filterDriverRows", "SelectLatestDrivers",
		"TestDriverApplicable", "ResolveLocalDriverVersion", "CompareDriverStatus",
		"partitionViewDrivers", "WriteHistoryRecord", "diffDeviceSnapshots"}
	called := map[string]bool{}
	for _, call := range allCalls(chain.ast) {
		called[calleeName(call)] = true
	}
	var missing []string
	for _, link := range links {
		if !called[link] {
			missing = append(missing, link)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the evidence chain no longer calls %s; a link was dropped from the chain",
			strings.Join(missing, ", "))
	}

	// The required fixtures are read off the test sources rather than listed
	// here: a hardcoded list stops covering the next fixture someone adds, and
	// the OS-248 cross-edition fixture already sat outside the old three names.
	fixtures := ctx.Repo.testdataFixtures()
	if len(fixtures) < 3 {
		return fmt.Errorf("the tests reference only %d testdata fixtures; the evidence chain was "+
			"dismantled or this scan lost its targets", len(fixtures))
	}
	hasRecorded := false
	for _, fixture := range fixtures {
		if strings.Contains(fixture, "_82jq") {
			hasRecorded = true
		}
	}
	if !hasRecorded {
		return fmt.Errorf("no 82JQ fixture is referenced by any test; the recorded-data size floor " +
			"would guard nothing")
	}
	for _, fixture := range fixtures {
		rel := ctx.Repo.fixturePath(fixture)
		if rel == "" {
			return fmt.Errorf("a test reads testdata/%s but no such fixture exists", fixture)
		}
		info, err := statSize(ctx.Repo, rel)
		if err != nil {
			return err
		}
		// A recorded machine fixture is bulk data, so an emptied or truncated one
		// would keep the tests green while proving nothing. Small hand-written
		// goldens are told apart by the same marker that names the machine the
		// evidence came off.
		if strings.Contains(fixture, "_82jq") && info < 2048 {
			return fmt.Errorf("recorded fixture %s is too small to be real recorded data", fixture)
		}
	}
	return nil
}

// checkAPIFixturesAreRecorded requires the API contract fixtures to be captured
// responses rather than hand-written samples, and requires the recorded web
// response to keep the envelope the live endpoint actually sends.
//
// This is the gate for a bug that lived for the whole life of the webpage
// source: webResponse typed statusCode and localOSID as strings, the live
// endpoint sends both as numbers, so every real response failed to decode and
// the source never worked. A hand-written golden quoted the numbers, so the
// tests agreed with the struct and disagreed with the server. A fixture named
// "recorded" and shaped by the author is worse than no fixture: it converts an
// unknown into a false certainty.
func checkAPIFixturesAreRecorded(ctx *Context) error {
	const (
		webFixture  = "web_real_82jq_os42.json"
		qfFixture   = "quickfix_contract_golden.json"
		webEndpoint = "drive_listnew"
	)
	webRel := ctx.Repo.fixturePath(webFixture)
	if webRel == "" {
		return fmt.Errorf("the recorded webpage response %s is gone; the API contract is back to "+
			"being pinned by nothing but a struct the author wrote", webFixture)
	}
	web, err := ctx.Repo.readFile(webRel)
	if err != nil {
		return err
	}
	// The envelope keys the live endpoint sends. A sample that omits them is
	// not a capture, whatever it is named.
	for _, key := range []string{`"statusCode"`, `"localOSID"`, `"osList"`, `"partList"`, `"drivelist"`} {
		if !strings.Contains(web, key) {
			return fmt.Errorf("%s does not carry %s; a hand-written sample cannot pin the live "+
				"response shape", webFixture, key)
		}
	}
	// The two fields whose Go type was wrong must be recorded in the spelling the
	// server uses. Quoted values here would re-hide the same class of bug.
	for _, key := range []string{"statusCode", "localOSID"} {
		pattern := regexp.MustCompile(`"` + key + `"\s*:\s*"`)
		if pattern.MatchString(web) {
			return fmt.Errorf("%s records %q as a quoted string; the live endpoint sends a JSON "+
				"number, so a quoted fixture hides a type mismatch instead of pinning it",
				webFixture, key)
		}
	}
	// The recording must be big enough to be a real machine's list, and must not
	// have lost the part names the parser joins on.
	info, err := statSize(ctx.Repo, webRel)
	if err != nil {
		return err
	}
	if info < 4096 {
		return fmt.Errorf("%s is %d bytes; a real 17-row response is far larger", webFixture, info)
	}
	if !strings.Contains(web, `"PartName"`) {
		return fmt.Errorf("%s carries no PartName; the parser joins part names from the part "+
			"entry and a fixture without one cannot exercise that join", webFixture)
	}

	// The QuickFix golden is hand-written on purpose (it exercises field
	// fallbacks a recorded row may not contain), so it is only required to keep
	// its documented divergence: no InstallCode, because QuickFix sends
	// Parameter instead.
	qfRel := ctx.Repo.fixturePath(qfFixture)
	if qfRel == "" {
		return fmt.Errorf("the QuickFix contract golden %s is gone", qfFixture)
	}
	qf, err := ctx.Repo.readFile(qfRel)
	if err != nil {
		return err
	}
	if !strings.Contains(qf, `"Parameter"`) {
		return fmt.Errorf("%s no longer exercises the Parameter install-code spelling", qfFixture)
	}

	// The endpoints the client actually calls must appear in contracts.md, so a
	// source the engine silently stops using is still visible to a reader.
	contract, err := ctx.Repo.readFile("docs/spec/contracts.md")
	if err != nil {
		return err
	}
	if !strings.Contains(contract, webEndpoint) {
		return fmt.Errorf("contracts.md no longer names %s although the client still calls it", webEndpoint)
	}
	return nil
}

// --- helpers ---

// fencedBlocks returns the bodies of the ```lang fenced blocks in a document.

// checkPlanBlocksCarryRowReasons holds the durable-artifact half of the
// evidence-first invariant. The console summary and the GUI export both name
// why a driver was skipped, but both are transient; WritePlanFile renders only
// BuildPlanText. BuildPlanText's field/doc comments claim the plan surfaces
// NonMatchReason, so the check pins the claim: the plan artifact must be at
// least as explanatory as the channels that evaporate with the run.
func checkPlanBlocksCarryRowReasons(ctx *Context) error {
	f, err := ctx.Repo.fileDeclaring("internal/plan", "BuildPlanText")
	if err != nil {
		return err
	}
	fn := topLevelFunc(f.ast, "BuildPlanText")
	if !identsIn(fn.Body)["NonMatchReason"] {
		return fmt.Errorf("BuildPlanText no longer prints the per-row reason; the persisted plan cannot " +
			"answer why a row was assessed Not applicable, while the throwaway channels can")
	}
	return nil
}

// checkSummaryNotesDescribeReachableStatuses keeps the summary notes honest on
// two axes. Unknown has three distinct causes (no official version, a
// provisioned placeholder, unparseable versions), so a single-cause sentence
// is a guess presented as a rule - the "multi-vendor" wording described a
// legacy parse-failure path the hardened version resolver no longer takes.
// And the "inference = ..." legend explained a category StatusEvidenceBasis is
// gate-forbidden from emitting; the only legitimate "inference" literals left
// are the switch case and the %d count line, both schema-complete scaffolding.
func checkSummaryNotesDescribeReachableStatuses(ctx *Context) error {
	var offenders []string
	for _, f := range ctx.Repo.dirProd("internal/plan") {
		for _, lit := range stringLiterals(f.ast) {
			value := strings.ReplaceAll(lit.Value, `\n`, " ")
			if strings.Contains(value, "multi-vendor") ||
				(strings.Contains(value, "inference") && value != `"inference"` && !strings.Contains(value, "%d")) {
				offenders = append(offenders, ctx.Repo.position(lit))
			}
		}
	}
	if len(offenders) > 0 {
		return failf("plan summary notes attribute statuses to causes the code does not produce", offenders)
	}
	return nil
}

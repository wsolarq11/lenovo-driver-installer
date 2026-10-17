package gate

// The document side of these comparisons is text, so it is read with a pattern.
// The Go side is read with the parser, which is the whole point: a tag written
// in a doc comment must not enter the engine's field set.

// The four checks in this file pin a Go source artifact against its human copy
// in docs/spec/contracts.md, or against the tests that exercise it. Each used to
// read the Go side as text, which made a comment part of the artifact: a doc
// comment spelling `json:"GhostField"` was scanned as a real field, and a
// comment naming a chain step satisfied the requirement that the step be called.
// The Go side is read through the parser here, so only declarations count.

import (
	"fmt"
	"regexp"
	"strings"
)

// checkJSONContractsMatchDocs holds contracts.md's two JSON blocks against the
// engine's own tags. export_test.go's golden locks the encoder, not the human
// copy, so without this the two can drift with every test still green.

func checkJSONContractsMatchDocs(ctx *Context) error {
	contract, err := ctx.Repo.readFile("docs/spec/contracts.md")
	if err != nil {
		return err
	}
	blocks := fencedBlocks(contract, "json")
	if len(blocks) != 2 {
		return fmt.Errorf("contracts.md must carry exactly two JSON contract blocks (driver view, "+
			"rollback offer), found %d", len(blocks))
	}
	documented := map[string]bool{}
	for _, block := range blocks {
		for _, name := range jsonKeysInText(block) {
			documented[name] = true
		}
	}

	engine := map[string]bool{}
	export, err := ctx.Repo.fileAt("internal/app/export.go")
	if err != nil {
		return err
	}
	for _, name := range jsonTagsInFile(export.ast) {
		engine[name] = true
	}
	model, err := ctx.Repo.fileWithName("internal/model", "OSListEntry")
	if err != nil {
		return fmt.Errorf("%s; the export OsList field lost its shape", err)
	}
	osTags, ok := jsonTagsInType(model.ast, "OSListEntry")
	if !ok {
		return fmt.Errorf("model.OSListEntry is no longer a struct; the export OsList field lost its shape")
	}
	for _, name := range osTags {
		engine[name] = true
	}
	rollback, err := ctx.Repo.fileAt("internal/app/rollback.go")
	if err != nil {
		return err
	}
	for _, name := range jsonTagsInFile(rollback.ast) {
		engine[name] = true
	}

	if len(engine) < 20 {
		return fmt.Errorf("the engine scan found only %d JSON fields; the tag scan is broken", len(engine))
	}
	return compareSets("JSON contract", engine, documented)
}

// checkCLIExitCodesMatchDocs holds the numeric exit codes in the help text
// against the table in contracts.md. The help text is the machine source; the
// table is the copy a reader sees.
func checkCLIExitCodesMatchDocs(ctx *Context) error {
	help, err := ctx.Repo.fileWithName("internal/app", "helpText")
	if err != nil {
		return err
	}
	text, ok := stringConst(help.ast, "helpText")
	if !ok {
		return fmt.Errorf("internal/app/help.go must declare helpText as a string constant")
	}
	block, ok := sectionAfter(text, "Exit codes:")
	if !ok {
		return fmt.Errorf("help.go exit-code block not found")
	}
	helpCodes := numbersAtLineStart(block)
	if len(helpCodes) == 0 {
		return fmt.Errorf("help.go parsed zero exit codes")
	}

	contract, err := ctx.Repo.readFile("docs/spec/contracts.md")
	if err != nil {
		return err
	}
	contractCodes := tableCodes(contract)
	if len(contractCodes) == 0 {
		return fmt.Errorf("contracts.md parsed zero exit codes")
	}
	return compareSets("exit-code", toSet(helpCodes), toSet(contractCodes))
}

// checkCLIArtifactPathsMatchDocs holds the artifact paths the engine declares
// against contracts.md and against every name the PowerShell layer spells. The
// WPF re-derives the plan and offer paths rather than asking the engine, so a
// renamed artifact would leave the GUI opening a path nothing writes.
func checkCLIArtifactPathsMatchDocs(ctx *Context) error {
	help, err := ctx.Repo.fileWithName("internal/app", "helpText")
	if err != nil {
		return err
	}
	helpText, ok := stringConst(help.ast, "helpText")
	if !ok {
		return fmt.Errorf("internal/app/help.go must declare helpText as a string constant")
	}
	helpPaths := artifactPathsAtLineStart(helpText)
	if len(helpPaths) < 3 {
		return fmt.Errorf("help.go parsed fewer than 3 audit artifact paths")
	}

	contract, err := ctx.Repo.readFile("docs/spec/contracts.md")
	if err != nil {
		return err
	}
	contractPaths := artifactPathsAtLineStart(contract)
	if len(contractPaths) < 3 {
		return fmt.Errorf("contracts.md parsed fewer than 3 audit artifact paths")
	}
	if err := compareSets("artifact-path", toSet(helpPaths), toSet(contractPaths)); err != nil {
		return err
	}

	// The offer file's name has a machine source of its own: the constant the
	// writer uses. help.go, contracts.md and the WPF layer are copies of it.
	rollback, err := ctx.Repo.fileAt("internal/app/rollback.go")
	if err != nil {
		return err
	}
	offerName, ok := stringConst(rollback.ast, "rollbackOfferFileName")
	if !ok {
		return fmt.Errorf("rollbackOfferFileName is gone; the offer artifact lost its machine name")
	}
	if !strings.Contains(helpText, offerName) {
		return fmt.Errorf("help.go's Artifacts block does not declare %s although the GUI opens it", offerName)
	}

	declared := toSet(artifactNamesIn(helpText))
	var stray []string
	for _, f := range ctx.Repo.files {
		if !strings.HasSuffix(f.relPath, ".ps1") {
			continue
		}
		text, err := ctx.Repo.readFile(f.relPath)
		if err != nil {
			return err
		}
		for _, name := range artifactNamesIn(text) {
			if !declared[name] {
				stray = append(stray, fmt.Sprintf("%s (%s)", name, f.relPath))
			}
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("PowerShell spells artifact names the engine does not declare: %s",
			strings.Join(dedupe(stray), ", "))
	}
	return nil
}

// checkArchitectureNetworkBoundary holds architecture.md's claim about which
// packages may reach the network against the import graph itself. The claim is
// the human-readable half of invariant 1; the code half is pinned by the
// zero-network check. They had drifted apart: the spec listed api among the
// deterministic packages while api imports net/http and net/url. A reader
// trusting the spec would have believed a network client was side-effect free.
//
// The comparison is a set equality, so both directions are caught: a package
// that imports the network without being named, and a name the doc grants that
// no longer imports it.
func checkArchitectureNetworkBoundary(ctx *Context) error {
	doc, err := ctx.Repo.readFile("docs/spec/architecture.md")
	if err != nil {
		return err
	}
	const anchor = "**联网包**"
	line, ok := lineContaining(doc, anchor)
	if !ok {
		return fmt.Errorf("architecture.md no longer states which packages may touch the network; " +
			"the boundary is unstated")
	}
	listed := parenthesisedNames(line)
	if len(listed) == 0 {
		return fmt.Errorf("architecture.md's network-boundary line names no package; the claim is unreadable")
	}

	actual := map[string]bool{}
	for _, f := range ctx.Repo.prod() {
		if !under(f.dir) {
			continue
		}
		for _, spec := range f.ast.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if path == "net/http" || path == "net/url" {
				actual[strings.TrimPrefix(f.dir, "internal/")] = true
			}
		}
	}
	if len(actual) == 0 {
		return fmt.Errorf("no production package imports the network; the boundary check has nothing to " +
			"compare and would pass vacuously")
	}
	// compareSets labels its first argument "engine only" and its second
	// "docs only", so the import graph is passed first: a package that imports
	// the network without being named reads as "engine only", which is the
	// direction that widens the boundary silently.
	return compareSets("network-boundary", actual, toSet(listed))
}

// lineContaining returns the first line of text that contains the anchor.
func lineContaining(text, anchor string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, anchor) {
			return line, true
		}
	}
	return "", false
}

// parenthesisedNames returns the backticked names inside the first （...） that
// follows the anchor. These documents list a claim's subjects that way, so it is
// where a claim's name list is read.
func parenthesisedNames(text string) []string {
	inner, ok := parenthesised(text, "（")
	if !ok {
		return nil
	}
	return backtickedNames(inner)
}

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

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

// --- helpers ---

// fencedBlocks returns the bodies of the ```lang fenced blocks in a document.

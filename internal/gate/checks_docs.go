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
	rollback, err := ctx.Repo.fileWithName("internal/app", "rollbackOffer")
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
// against contracts.md and against every name the launcher scripts spell. A
// renamed artifact would otherwise leave a launcher opening a path nothing
// writes.
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
	// writer uses. help.go and contracts.md are copies of it.
	rollback, err := ctx.Repo.fileWithName("internal/app", "rollbackOfferFileName")
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
		if !strings.HasSuffix(f.relPath, ".bat") {
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
		return fmt.Errorf("launcher scripts spell artifact names the engine does not declare: %s",
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

package gate

// The checks in this file state shapes the program must have, and read those
// shapes from the syntax tree. Each was once a PowerShell regex over Go source,
// where a comment could satisfy a rule or break one, a branch matcher depended
// on the indentation the formatter produced, and a grouped `const (...)` or
// `var (...)` declaration was invisible to a line-anchored pattern.

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// readFile returns a repository file's bytes as text. The Go rules read Go
// source through the parser; the few non-Go artifacts a rule pins (the WPF
// markup) are read here. Decoding is explicit UTF-8, so the verdict does not
// depend on the host's default code page the way a PowerShell Get-Content does.
func (r *Repo) readFile(rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// testDeclaring returns the declaration of the named test function in dir.
// A test that exists is a declaration, so the rule is asked of the syntax tree
// rather than of the text: a comment naming the test can no longer stand in for
// the test itself, and a rename cannot hide behind a leftover mention.
func (r *Repo) testDeclaring(dir, name string) (*ast.FuncDecl, error) {
	for _, f := range r.dirFiles(dir) {
		if !f.isTest {
			continue
		}
		if fd := topLevelFunc(f.ast, name); fd != nil {
			return fd, nil
		}
	}
	return nil, fmt.Errorf("%s is gone from %s; the rule it guards is unguarded", name, dir)
}

// checkAutomaticSetExcludesDowngrade holds invariant 3: the automatic install
// set is an allow-list, and every exit onto it projects the same predicate.
func checkAutomaticSetExcludesDowngrade(ctx *Context) error {
	membership, err := ctx.Repo.fileDeclaring("internal/model", "InAutomaticInstallSet")
	if err != nil {
		return err
	}
	body := topLevelFunc(membership.ast, "InAutomaticInstallSet").Body
	// An allow-list is ==. A != comparison silently re-admits downgrade and
	// no-op statuses, and asking the syntax tree means a comment explaining the
	// rule cannot be mistaken for a breach of it.
	if hasBinaryOp(body, token.NEQ) {
		return fmt.Errorf("InAutomaticInstallSet must be an explicit allow-list (==); a \"!=\" comparison " +
			"silently re-admits downgrade/no-op statuses")
	}
	mentioned := identsIn(body)
	for _, required := range []string{"StatusUpdate", "StatusNotInstalled"} {
		if !mentioned[required] {
			return fmt.Errorf("InAutomaticInstallSet must admit %s explicitly", required)
		}
	}

	// view.go partitions the presented set; it must ask the one membership
	// authority rather than compare statuses itself.
	view, err := ctx.Repo.fileAt("internal/app/view.go")
	if err != nil {
		return err
	}
	if !callsQualified(view.ast, "model.InAutomaticInstallSet") {
		return fmt.Errorf("view.go partitions the automatic install set itself; it must call " +
			"model.InAutomaticInstallSet so the set has one membership rule")
	}
	if loose := binaryOpsWithOperand(view.ast, token.NEQ, "model.StatusNotApplicable"); len(loose) > 0 {
		return failf("view.go partitions the automatic install set with a loose comparison; use "+
			"model.InAutomaticInstallSet", positions(ctx, loose))
	}

	// The interactive prompt must describe the set it actually installs.
	interactive, err := ctx.Repo.fileAt("internal/app/interactive.go")
	if err != nil {
		return err
	}
	for _, lit := range stringLiteralValues(interactive.ast) {
		if strings.Contains(lit, "all-applicable") || strings.Contains(lit, "all applicable") {
			return fmt.Errorf("the interactive prompt still advertises the old \"all applicable\" set " +
				"whose membership changed")
		}
	}

	// The GUI export is a second exit onto the same set: the WPF install-all
	// button filters on IsApplicable and passes the codes back, bypassing the
	// partition entirely. It must project the same predicate or the GUI keeps
	// downgrading what the CLI refuses.
	export, err := ctx.Repo.fileAt("internal/app/export.go")
	if err != nil {
		return err
	}
	if !assignsCallToKey(export.ast, "IsApplicable", "model.InAutomaticInstallSet") {
		return fmt.Errorf("export.go must project model.InAutomaticInstallSet onto IsApplicable; the GUI " +
			"would otherwise act on a set the CLI does not")
	}

	// The button label makes the same claim to the operator.
	xaml, err := ctx.Repo.readFile("wpf/window.xaml")
	if err != nil {
		return err
	}
	if strings.Contains(xaml, "安装全部可安装") {
		return fmt.Errorf("the WPF install-all button still says \"安装全部可安装\"; the set no longer " +
			"contains every applicable driver")
	}
	return nil
}

// checkOneComparisonPrimitive holds invariant 4.1: ord(a,b) has four values,
// not three. Folding Undecided into Less is how an unmeasurable value becomes a
// fact, because a missing version reads as older than everything and would be
// handed to the automatic set.
func checkOneComparisonPrimitive(ctx *Context) error {
	version, err := ctx.Repo.fileAt("internal/compare/version.go")
	if err != nil {
		return err
	}
	if !hasTopLevelName(version.ast, "VersionOrder") {
		return fmt.Errorf("VersionOrder is gone; the undecided outcome is lost again")
	}
	// The four outcomes live in a grouped const block, which a line-anchored
	// `^(var|const) Name` pattern never saw.
	declared := declaredNames(version.ast)
	for _, outcome := range []string{"OrderUndecided", "orderLess", "OrderEqual", "orderGreater"} {
		if !declared[outcome] {
			return fmt.Errorf("VersionOrder lost the %s outcome", outcome)
		}
	}
	order := topLevelFunc(version.ast, "Order")
	if order == nil {
		return fmt.Errorf("the single Order comparator is gone")
	}
	if !returnsUndecidedOnNil(order.Body) {
		return fmt.Errorf("Order must return OrderUndecided for a missing version before it considers " +
			"direction")
	}
	if !hasTopLevelName(version.ast, "ResolveComparableVersion") {
		return fmt.Errorf("ResolveComparableVersion is gone; callers will compare raw strings again")
	}

	// The recheck and the compare pass must speak through that one primitive.
	binding, err := ctx.Repo.fileAt("internal/app/install_binding.go")
	if err != nil {
		return err
	}
	if raw := binaryOpsWithOperand(binding.ast, token.EQL, "packageVersion", "after", "before"); len(raw) > 0 {
		return fmt.Errorf("the post-install recheck is comparing raw version strings again; the WLAN rows " +
			"carry \"Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314\" in Version, which " +
			"no local version can equal, so those drivers could never be confirmed bound")
	}
	if !callsQualified(binding.ast, "compare.Order") {
		return fmt.Errorf("the post-install recheck no longer uses the one comparator")
	}
	matching, err := ctx.Repo.fileAt("internal/compare/matching.go")
	if err != nil {
		return err
	}
	if callsOn(matching.ast, "localVersion", "Compare") {
		return fmt.Errorf("CompareDriverStatus calls Version.Compare directly again; the undecided outcome " +
			"would be folded into a direction")
	}
	if !callsWithArgs(matching.ast, "Order", "localVersion", "remoteVersion") {
		return fmt.Errorf("CompareDriverStatus no longer routes through Order")
	}
	return nil
}

// checkSelectionOrderTotal holds that the selection sort is a total order.
// PartID alone is not a key: on 82JQ one PartID covers several groups, so a
// PartID-only stable sort left the survivors in map-iteration order and
// consecutive exports of identical input disagreed on row order.
func checkSelectionOrderTotal(ctx *Context) error {
	selection, err := ctx.Repo.fileDeclaring("internal/compare", "SelectLatestDrivers")
	if err != nil {
		return err
	}
	if callsWithArgs(selection.ast, "sort.SliceStable", "selected") {
		return fmt.Errorf("selection still uses SliceStable on a non-total key; equal prefixes fall back to " +
			"map order")
	}
	sortCall := firstCallWithArgs(selection.ast, "sort.Slice", "selected")
	if sortCall == nil {
		return fmt.Errorf("could not locate the SelectLatestDrivers sort; ordering is unguarded")
	}
	mentioned := identsIn(sortCall)
	for _, key := range []string{"PartID", "DriverName", "DriverCode"} {
		if !mentioned[key] {
			return fmt.Errorf("the selection sort does not tie-break on %s; equal prefixes would fall back "+
				"to map order", key)
		}
	}
	for _, name := range []string{"TestSelectionOrderIsDeterministic", "TestFirmwareRowsAreDroppedUnlessRequested"} {
		if _, err := ctx.Repo.testDeclaring("internal/app", name); err != nil {
			return fmt.Errorf("%s; determinism is unguarded", err)
		}
	}
	return nil
}

// checkAutomaticSetBackedByMeasurements holds invariant 3.1: which statuses may
// be installed unattended, and how strongly each status is evidenced, are two
// decisions that must not drift apart.
func checkAutomaticSetBackedByMeasurements(ctx *Context) error {
	model, err := ctx.Repo.fileDeclaring("internal/model", "StatusEvidenceBasis")
	if err != nil {
		return err
	}
	for _, name := range []string{"TestAutomaticSetStatusesAreMeasured",
		"TestNonFactStatusesStayOutOfTheAutomaticSet"} {
		if _, err := ctx.Repo.testDeclaring("internal/model", name); err != nil {
			return fmt.Errorf("%s; widening the automatic set would no longer have to be argued", err)
		}
	}
	fn := topLevelFunc(model.ast, "StatusEvidenceBasis")
	factStatuses := map[string]bool{}
	for _, clause := range switchCases(fn.Body) {
		for _, returned := range clause.returns {
			if returned != "fact" {
				continue
			}
			for _, value := range clause.values {
				factStatuses[value] = true
			}
		}
	}
	if len(factStatuses) == 0 {
		return fmt.Errorf("StatusEvidenceBasis no longer returns fact from an explicit case list")
	}
	for _, status := range []string{"StatusUpdate", "StatusUpToDate", "StatusNotInstalled"} {
		if !factStatuses[status] {
			return fmt.Errorf("automatic-set member %s is not routed to fact by StatusEvidenceBasis", status)
		}
	}
	for _, lit := range stringLiteralValues(fn.Body) {
		if lit == "inference" {
			return fmt.Errorf("StatusEvidenceBasis still emits inference; every status it can name is " +
				"either measured or undecided")
		}
	}
	return nil
}

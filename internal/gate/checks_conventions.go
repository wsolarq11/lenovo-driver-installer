package gate

// The coding-convention single-source rule. The convention documents used to
// live in .trellis/spec/, which .gitignore excludes: a fresh clone had no such
// directory, and the gate could not read a file it was told was authoritative.
// The copy that could not be read drifted (it still listed internal/api among
// the deterministic packages while api imports net/http), and nothing noticed.
//
// Two directions are checked. The authority must exist under a tracked path and
// be reachable from the entry document, so a reader is never pointed at a
// pointer chain that ends nowhere. And no ignored directory may carry a second
// copy of a convention document, so the unreadable copy cannot come back.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// conventionDocs are the coding-convention files whose single authority is
// docs/spec/coding/. They are named here because the rule must notice one being
// deleted, not only a directory being renamed.
var conventionDocs = []string{
	"docs/spec/coding/index.md",
	"docs/spec/coding/directory-structure.md",
	"docs/spec/coding/error-handling.md",
	"docs/spec/coding/logging-guidelines.md",
	"docs/spec/coding/quality-guidelines.md",
	"docs/spec/coding/single-source.md",
	"docs/spec/coding/guides/index.md",
}

// ignoredTrees are directories whose contents are not in a clone. A convention
// document inside one of them is unreadable in exactly the situation where
// authority matters, so a copy there is drift by construction.
var ignoredTrees = []string{".trellis", ".agents", ".dsh", ".zcode"}

func checkConventionsAreTracked(ctx *Context) error {
	for _, rel := range conventionDocs {
		if _, err := ctx.Repo.readFile(rel); err != nil {
			return fmt.Errorf("%s is gone; the coding conventions lost their single authority", rel)
		}
	}

	// The entry document must name the authority, so a reader landing on
	// README or docs/spec/index.md is sent to a path that exists.
	index, err := ctx.Repo.readFile("docs/spec/index.md")
	if err != nil {
		return err
	}
	if !strings.Contains(index, "docs/spec/coding/") {
		return fmt.Errorf("docs/spec/index.md no longer points at docs/spec/coding/; the convention " +
			"documents have no entrance")
	}

	// No ignored tree may hold a convention document. The names are matched as a
	// set rather than as full paths: the point is that a second copy exists, not
	// what it was called.
	names := map[string]bool{}
	for _, rel := range conventionDocs {
		names[filepath.Base(rel)] = true
	}
	var offenders []string
	for _, tree := range ignoredTrees {
		base := filepath.Join(ctx.Repo.root, tree)
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !names[d.Name()] {
				return nil
			}
			// index.md is a generic name shared with Trellis workspace indexes,
			// which are working records, not conventions. A convention copy lives
			// under a spec/ tree, so the parent directory is part of the match.
			if filepath.Base(filepath.Dir(path)) != "backend" && filepath.Base(filepath.Dir(path)) != "guides" {
				return nil
			}
			// A pointer stub is the sanctioned shape: it names the authority and
			// carries no copy of it. A file this large is a copy.
			info, statErr := d.Info()
			if statErr != nil || info.Size() <= 512 {
				return nil
			}
			rel, relErr := filepath.Rel(ctx.Repo.root, path)
			if relErr != nil {
				return nil
			}
			offenders = append(offenders, filepath.ToSlash(rel))
			return nil
		})
	}
	if len(offenders) > 0 {
		return fmt.Errorf("a gitignored tree carries a second copy of a coding-convention document, "+
			"which no clone has and no gate reads: %s", strings.Join(offenders, ", "))
	}
	return nil
}

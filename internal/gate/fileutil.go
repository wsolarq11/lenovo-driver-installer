package gate

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The disk-touching helpers the fixture checks need. Everything else in this
// package reads the tree the loader already parsed; a fixture is data rather
// than Go source, so it is found and measured here.

// findFixture returns the repository-relative paths of every file with the
// given name under a testdata directory.
func findFixture(root, name string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != name || filepath.Base(filepath.Dir(path)) != "testdata" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(out)
	return out, nil
}

// statSize returns the size in bytes of a repository-relative file.
func statSize(r *Repo, rel string) (int64, error) {
	info, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(rel)))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// sorted returns a sorted copy, so a failure message lists its findings in a
// stable order rather than in map-iteration order.
func sorted(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}

// projectPs1Files returns the repository-relative paths of the PowerShell files
// the project owns. The loader already knows every tracked path it parsed, but
// a .ps1 is not Go source, so the tree is walked for them here.
func (r *Repo) projectPs1Files() ([]string, error) {
	var out []string
	err := filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".ps1") {
			return nil
		}
		rel, err := filepath.Rel(r.root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(out)
	return out, nil
}

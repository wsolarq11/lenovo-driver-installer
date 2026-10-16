// Command gatecheck runs the gate's Go source checks from the command line.
//
// The checks themselves live in internal/gate, a stdlib-only package that never
// runs a process; this binary owns the one process boundary they need, which is
// asking the Go toolchain for the export data of the module's dependencies.
// scripts/verify.ps1 builds it and loops one Assert-Step per check name, so the
// gate keeps its named, counted steps while the bodies live in compiled Go.
//
// Usage:
//
//	gatecheck -list
//	gatecheck -check NAME [-root DIR]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"lenovo-driver/internal/gate"
)

func main() {
	list := flag.Bool("list", false, "print every check name, one per line")
	check := flag.String("check", "", "run one named check")
	root := flag.String("root", "", "repository root (default: the module directory)")
	goExe := flag.String("go", "go", "the Go toolchain to ask for dependency export data")
	flag.Parse()

	// -list answers from the registry alone. It deliberately reads no repository:
	// the gate builds its step list from this output, and a step list that
	// disappears whenever the tree fails to parse would hide every check behind
	// one unrelated failure instead of reporting each rule by name.
	if *list {
		for _, name := range gate.Names() {
			fmt.Println(name)
		}
		return
	}
	if *check == "" {
		fatal(fmt.Errorf("-check is required (or use -list)"))
	}

	if *root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
		}
		modRoot, err := moduleRoot(cwd)
		if err != nil {
			fatal(err)
		}
		*root = modRoot
	}
	*root = filepath.Clean(*root)

	repo, err := gate.Load(*root)
	if err != nil {
		fatal(err)
	}

	exports, err := loadExports(*root, *goExe)
	if err != nil {
		fatal(err)
	}

	ctx := &gate.Context{Repo: repo, Exports: exports}
	if err := gate.Run(ctx, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// moduleRoot walks up from dir to the go.mod.
func moduleRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

// loadExports asks the Go toolchain for the export data of the module's
// dependencies. Our own packages are checked from source by internal/gate; only
// this map of outside packages is handed over.
func loadExports(root, goExe string) (gate.ExportData, error) {
	cmd := exec.Command(goExe, "list", "-export", "-deps", "-test", "-f", "{{.ImportPath}}\t{{.Export}}", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list -export failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("go list -export failed: %w", err)
	}
	exports := gate.ExportData{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		path, filePath, ok := strings.Cut(line, "\t")
		if !ok || path == "" || filePath == "" {
			continue
		}
		exports[path] = filePath
	}
	return exports, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

package app

// The CLI surface: option parsing and the contract checks that reject
// combinations the engine cannot honor. Kept apart from the run loop so the
// contract reads as one piece.

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// parseOptions parses the Go CLI with the PS1 parameter names.
func parseOptions(args []string) (*Options, error) {
	opts := &Options{}
	fs := flag.NewFlagSet("lenovo-driver", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.DryRun, "DryRun", false, "")
	fs.BoolVar(&opts.IncludeBios, "IncludeBios", false, "")
	fs.BoolVar(&opts.LatestAcrossOS, "LatestAcrossOS", false, "")
	fs.BoolVar(&opts.CurrentOSOnly, "CurrentOSOnly", false, "")
	fs.StringVar(&opts.TargetOS, "TargetOS", "", "")
	fs.BoolVar(&opts.DownloadOnly, "DownloadOnly", false, "")
	fs.BoolVar(&opts.SkipHashCheck, "SkipHashCheck", false, "")
	fs.BoolVar(&opts.SkipSignatureCheck, "SkipSignatureCheck", false, "")
	fs.BoolVar(&opts.Elevated, "Elevated", false, "")
	fs.BoolVar(&opts.Help, "Help", false, "")
	fs.StringVar(&opts.Model, "Model", "", "")
	fs.StringVar(&opts.DownloadDir, "DownloadDir", "", "")
	fs.StringVar(&opts.GuiExportPath, "GuiExportPath", "", "")
	fs.StringVar(&opts.GuiInstallCodes, "GuiInstallCodes", "", "")
	fs.StringVar(&opts.Rollback, "Rollback", "", "")
	fs.StringVar(&opts.AuditPath, "Audit", "", "")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	return opts, nil
}

func (opts *Options) TargetOSActive() bool {
	return opts.TargetOS != ""
}

// Validate checks CLI contract combinations and returns the PS1 exit code 2 on error.
func (opts *Options) Validate() error {
	if opts.CurrentOSOnly && opts.LatestAcrossOS {
		return fmt.Errorf("-CurrentOSOnly and -LatestAcrossOS cannot be used together")
	}
	if opts.TargetOS != "" && opts.CurrentOSOnly {
		return fmt.Errorf("-TargetOS and -CurrentOSOnly cannot be used together")
	}
	if opts.TargetOS != "" && opts.LatestAcrossOS {
		return fmt.Errorf("-TargetOS and -LatestAcrossOS cannot be used together")
	}
	if opts.Rollback != "" && (opts.GuiExportPath != "" || opts.GuiInstallCodes != "" || opts.DryRun || opts.DownloadOnly) {
		return fmt.Errorf("-Rollback cannot be combined with export, install, or dry-run modes")
	}
	if opts.AuditPath != "" && (opts.Rollback != "" || opts.GuiExportPath != "" || opts.GuiInstallCodes != "" || opts.DryRun || opts.DownloadOnly) {
		return fmt.Errorf("-Audit is a standalone mode and cannot be combined with rollback, export, install, or dry-run modes")
	}
	return nil
}

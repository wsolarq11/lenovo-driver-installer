package app

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/download"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

// Options mirrors the public CLI contract.
type Options struct {
	DryRun          bool
	IncludeBios     bool
	LatestAcrossOS  bool
	CurrentOSOnly   bool
	TargetOS        string
	DownloadOnly    bool
	SkipHashCheck   bool
	Elevated        bool
	Help            bool
	Model           string
	DownloadDir     string
	GuiExportPath   string
	GuiInstallCodes string
}

// App owns the shared runtime dependencies.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  *bufio.Reader

	LogPath     string
	PlanPath    string
	HistoryPath string
	startedAt   time.Time

	APIClient  *api.Client
	Downloader *download.Downloader
}

// New returns a configured app using TEMP artifact paths.
func New(stdout, stderr io.Writer, stdin io.Reader) *App {
	return &App{
		Stdout:      stdout,
		Stderr:      stderr,
		Stdin:       bufio.NewReader(stdin),
		LogPath:     filepath.Join(os.TempDir(), "lenovo_driver_install.log"),
		PlanPath:    filepath.Join(os.TempDir(), "lenovo_driver_plan.txt"),
		HistoryPath: filepath.Join(os.TempDir(), "lenovo_driver_history.csv"),
		APIClient:   api.NewClient(),
		Downloader:  download.NewDownloader(),
		startedAt:   time.Now(),
	}
}

// ParseOptions parses the Go CLI with the PS1 parameter names.
func ParseOptions(args []string) (*Options, error) {
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
	fs.BoolVar(&opts.Elevated, "Elevated", false, "")
	fs.BoolVar(&opts.Help, "Help", false, "")
	fs.StringVar(&opts.Model, "Model", "", "")
	fs.StringVar(&opts.DownloadDir, "DownloadDir", "", "")
	fs.StringVar(&opts.GuiExportPath, "GuiExportPath", "", "")
	fs.StringVar(&opts.GuiInstallCodes, "GuiInstallCodes", "", "")
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
	return nil
}

// Run executes the CLI and returns the process exit code.
func (a *App) Run(args []string) int {
	opts, err := ParseOptions(args)
	if err != nil {
		fmt.Fprintln(a.Stderr, "Error: "+err.Error())
		return 2
	}
	if opts.Help {
		_, _ = io.WriteString(a.Stdout, HelpText)
		return 0
	}
	if err := opts.Validate(); err != nil {
		fmt.Fprintln(a.Stderr, "Error: "+err.Error())
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	admin, adminErr := inventory.IsAdministrator()
	if !opts.DryRun && !opts.DownloadOnly && opts.GuiExportPath == "" && !opts.Elevated && (!admin || adminErr != nil) {
		if relaunched, code := a.RelaunchElevated(args); relaunched {
			return code
		}
		a.Log(ctx, "Administrator privileges are required for installs. Use install_lenovo_drivers.bat or -Elevated after elevation.", "ERROR")
		return 1
	}

	a.Log(ctx, "=== Lenovo driver update started ===", "INFO")
	machine, err := inventory.GetMachineInfo(ctx)
	if err != nil {
		a.Log(ctx, "Machine lookup failed: "+err.Error(), "ERROR")
		return 1
	}
	osInfo, err := inventory.GetOSInfo(ctx)
	if err != nil {
		a.Log(ctx, "OS lookup failed: "+err.Error(), "ERROR")
		return 1
	}
	a.Log(ctx, "Machine model : "+machine.Model, "INFO")
	a.Log(ctx, "Serial number : "+machine.Serial, "INFO")
	a.Log(ctx, "System        : "+osInfo.Caption, "INFO")
	a.Log(ctx, "OS match key  : "+osInfo.OSName, "INFO")

	categoryID, err := a.APIClient.ResolveCategoryID(ctx, firstNonEmpty(opts.Model, machine.Model), machine.Serial)
	if err != nil || categoryID == "" {
		a.Log(ctx, "Could not resolve the Lenovo machine category. Use -Model \"82JQ\" if the automatic lookup fails: "+errText(err), "ERROR")
		return 1
	}
	a.Log(ctx, "Lenovo category ID : "+categoryID, "INFO")

	osResolution, err := a.APIClient.ResolveOSEntry(ctx, categoryID, osInfo.OSName, osInfo.Kind)
	if err != nil || osResolution == nil {
		a.Log(ctx, "Could not resolve the current OS entry from Lenovo. Use -LatestAcrossOS only after confirming the OS list is complete.", "ERROR")
		return 1
	}
	osList := osResolution.OSList
	osEntry := osResolution.OSEntry
	sysID := osEntry.OSID
	a.Log(ctx, "Matched OS entry : "+osEntry.OSName+" (OSID "+sysID+")", "INFO")
	if osResolution.Source == "QuickFix" {
		a.Log(ctx, "OS list source : QuickFix (webpage OS list unavailable)", "WARN")
	}

	targetOsEntry := (*model.OSListEntry)(nil)
	if opts.TargetOS != "" {
		targetOsEntry = resolveTarget(osList, opts.TargetOS)
		if targetOsEntry == nil {
			a.Log(ctx, "Could not resolve -TargetOS '"+opts.TargetOS+"' from the Lenovo OS list for this machine.", "ERROR")
			return 1
		}
	}
	listOsID := sysID
	if targetOsEntry != nil {
		listOsID = targetOsEntry.OSID
		a.Log(ctx, "Target OS entry : "+targetOsEntry.OSName+" (OSID "+listOsID+")", "INFO")
	} else if !opts.LatestAcrossOS && !opts.CurrentOSOnly && len(osList) > 1 {
		a.Log(ctx, "Current OS mode enabled (default). In the interactive menu, press t to switch supported OS lists.", "INFO")
	}

	localDevices, err := inventory.GetLocalDeviceSnapshot(ctx)
	if err != nil {
		a.Log(ctx, "Local device snapshot failed: "+err.Error(), "WARN")
		localDevices = nil
	}
	installedApps, err := inventory.GetInstalledApps(ctx)
	if err != nil {
		a.Log(ctx, "Installed application snapshot failed: "+err.Error(), "WARN")
		installedApps = nil
	}
	softwareSnapshot, err := inventory.GetSoftwareSnapshot(ctx, installedApps)
	if err != nil {
		a.Log(ctx, "Software snapshot failed: "+err.Error(), "WARN")
	}
	history := a.ReadHistory(ctx)

	view, err := a.CompareOSDriverView(ctx, opts, categoryID, listOsID, sysID, osList, localDevices, installedApps, softwareSnapshot, history)
	if err != nil {
		a.Log(ctx, "Driver comparison failed: "+err.Error(), "ERROR")
		return 1
	}

	if opts.GuiExportPath != "" {
		if err := a.ExportGUIView(opts.GuiExportPath, view, osList, sysID, machine, osInfo); err != nil {
			a.Log(ctx, "GUI export failed: "+err.Error(), "ERROR")
			return 1
		}
		a.Log(ctx, "GUI export : "+opts.GuiExportPath, "INFO")
		return 0
	}

	if opts.DryRun {
		a.Log(ctx, "Dry run finished. No files were downloaded or installed.", "INFO")
		return 0
	}

	var selected []*model.Driver
	if opts.GuiInstallCodes != "" {
		selected = selectByCodes(view.Selected, opts.GuiInstallCodes)
		if len(selected) == 0 {
			a.Log(ctx, "None of the GUI-selected driver codes matched the current list.", "ERROR")
			return 3
		}
	} else {
		selection := a.SelectInteractive(ctx, opts, view, categoryID, listOsID, sysID, osList, localDevices, installedApps, softwareSnapshot, history)
		if selection == nil {
			return 0
		}
		selected = selection
	}

	if err := a.InstallSelected(ctx, opts, selected, view.Source, categoryID, listOsID, osList); err != nil {
		a.Log(ctx, "Install failed: "+err.Error(), "ERROR")
		return 1
	}
	return 0
}

func (a *App) RelaunchElevated(args []string) (bool, int) {
	exe, err := os.Executable()
	if err != nil {
		return false, 1
	}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "''")+"'")
	}
	script := fmt.Sprintf(`$p = Start-Process -FilePath '%s' -ArgumentList @(%s) -Verb RunAs -Wait -PassThru; exit $p.ExitCode`, strings.ReplaceAll(exe, "'", "''"), strings.Join(quoted, ","))
	cmd := execPowershell(script)
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		code = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
	}
	return true, code
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

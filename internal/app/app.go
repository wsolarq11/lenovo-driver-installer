package app

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/compare"
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

// ViewContext carries the resolved runtime inputs shared by comparison,
// selection, export, and installation. It is treated as read-only after
// resolution; per-run scratch state (e.g. the OS driver cache) lives on App,
// not here, so messaging code cannot silently mutate shared inputs.
type ViewContext struct {
	Opts              Options
	CategoryID        string
	CurrentSystemOsID string
	OsList            []model.OSListEntry
	LocalDevices      []model.Device
	InstalledApps     []model.InstalledApp
	SoftwareSnapshot  model.SoftwareSnapshot
	History           []model.HistoryRecord
	Machine           inventory.MachineInfo
	OSInfo            inventory.OSInfo
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

	// osDriverCache memoizes fetched OS driver lists for the current run. It is
	// kept here (not on ViewContext) because it is mutable per-run scratch
	// state; ViewContext otherwise carries immutable resolved inputs. The mutex
	// guards the map so independent OS lists can be fetched in parallel.
	osDriverCache   map[string]api.SourceDrivers
	osDriverCacheMu sync.Mutex
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

	if done, code := a.maybeElevated(ctx, opts, args); done {
		return code
	}

	vc, code := a.resolveRuntime(ctx, opts)
	if code != 0 {
		return code
	}

	listOsID := vc.CurrentSystemOsID
	if opts.TargetOS != "" {
		target := compare.ResolveTargetOsEntry(vc.OsList, opts.TargetOS)
		if target == nil {
			a.Log(ctx, "Could not resolve -TargetOS '"+opts.TargetOS+"' from the Lenovo OS list for this machine.", "ERROR")
			return 1
		}
		listOsID = target.OSID
		a.Log(ctx, "Target OS entry : "+target.OSName+" (OSID "+listOsID+")", "INFO")
	} else if !opts.LatestAcrossOS && !opts.CurrentOSOnly && len(vc.OsList) > 1 {
		a.Log(ctx, "Current OS mode enabled (default). In the interactive menu, press t to switch supported OS lists.", "INFO")
	}

	view, err := a.CompareOSDriverView(ctx, vc, listOsID)
	if err != nil {
		a.Log(ctx, "Driver comparison failed: "+err.Error(), "ERROR")
		return 1
	}

	return a.runSelection(ctx, vc, view, listOsID)
}

// runSelection dispatches the export, dry-run, and select/install flows that
// follow a successful comparison view build.
func (a *App) runSelection(ctx context.Context, vc *ViewContext, view *DriverView, listOsID string) int {
	if vc.Opts.GuiExportPath != "" {
		if err := a.ExportGUIView(vc.Opts.GuiExportPath, vc, view); err != nil {
			a.Log(ctx, "GUI export failed: "+err.Error(), "ERROR")
			return 1
		}
		a.Log(ctx, "GUI export : "+vc.Opts.GuiExportPath, "INFO")
		return 0
	}

	if vc.Opts.DryRun {
		a.Log(ctx, "Dry run finished. No files were downloaded or installed.", "INFO")
		return 0
	}

	var selected []*model.Driver
	if vc.Opts.GuiInstallCodes != "" {
		selection := selectByCodes(view.Selected, vc.Opts.GuiInstallCodes)
		if len(selection.Missing) > 0 || len(selection.NotApplicable) > 0 || len(selection.Selected) == 0 {
			a.Log(ctx, "GUI-selected driver codes did not match the current list.", "ERROR")
			return 3
		}
		selected = selection.Selected
	} else {
		selection := a.SelectInteractive(ctx, vc, view, listOsID)
		if selection == nil {
			return 0
		}
		selected = selection
	}

	if err := a.InstallSelected(ctx, vc, selected, view.Source, listOsID); err != nil {
		a.Log(ctx, "Install failed: "+err.Error(), "ERROR")
		return 1
	}
	return 0
}

// maybeElevated reports whether the process should relaunch elevated (and, if
// so, returns the sub-process exit code). It is only active for install flows.
func (a *App) maybeElevated(ctx context.Context, opts *Options, args []string) (done bool, code int) {
	admin, adminErr := inventory.IsAdministrator()
	if opts.DryRun || opts.DownloadOnly || opts.GuiExportPath != "" || opts.Elevated || (admin && adminErr == nil) {
		return false, 0
	}
	if relaunched, relaunchCode := a.RelaunchElevated(args); relaunched {
		return true, relaunchCode
	}
	a.Log(ctx, "Administrator privileges are required for installs. Use install_lenovo_drivers.bat or -Elevated after elevation.", "ERROR")
	return true, 1
}

// resolveRuntime resolves the machine/OS identity, Lenovo category and OS
// entry, and the local inventory snapshot into a single shared context.
func (a *App) resolveRuntime(ctx context.Context, opts *Options) (*ViewContext, int) {
	a.Log(ctx, "=== Lenovo driver update started ===", "INFO")
	machine, err := inventory.GetMachineInfo(ctx)
	if err != nil {
		a.Log(ctx, "Machine lookup failed: "+err.Error(), "ERROR")
		return nil, 1
	}
	osInfo, err := inventory.GetOSInfo(ctx)
	if err != nil {
		a.Log(ctx, "OS lookup failed: "+err.Error(), "ERROR")
		return nil, 1
	}
	a.Log(ctx, "Machine model : "+machine.Model, "INFO")
	a.Log(ctx, "Serial number : "+machine.Serial, "INFO")
	a.Log(ctx, "System        : "+osInfo.Caption, "INFO")
	a.Log(ctx, "OS match key  : "+osInfo.OSName, "INFO")

	categoryID, err := a.APIClient.ResolveCategoryID(ctx, firstNonEmpty(opts.Model, machine.Model), machine.Serial)
	if err != nil || categoryID == "" {
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		a.Log(ctx, "Could not resolve the Lenovo machine category. Use -Model \"82JQ\" if the automatic lookup fails: "+reason, "ERROR")
		return nil, 1
	}
	a.Log(ctx, "Lenovo category ID : "+categoryID, "INFO")

	osResolution, err := a.APIClient.ResolveOSEntry(ctx, categoryID, osInfo.OSName, osInfo.Kind)
	if err != nil || osResolution == nil {
		a.Log(ctx, "Could not resolve the current OS entry from Lenovo. Use -LatestAcrossOS only after confirming the OS list is complete.", "ERROR")
		return nil, 1
	}
	sysID := osResolution.OSEntry.OSID
	a.Log(ctx, "Matched OS entry : "+osResolution.OSEntry.OSName+" (OSID "+sysID+")", "INFO")
	if osResolution.Source == "QuickFix" {
		a.Log(ctx, "OS list source : QuickFix (webpage OS list unavailable)", "WARN")
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

	return &ViewContext{
		Opts:              *opts,
		CategoryID:        categoryID,
		CurrentSystemOsID: sysID,
		OsList:            osResolution.OSList,
		LocalDevices:      localDevices,
		InstalledApps:     installedApps,
		SoftwareSnapshot:  softwareSnapshot,
		History:           history,
		Machine:           machine,
		OSInfo:            osInfo,
	}, 0
}

func (a *App) RelaunchElevated(args []string) (bool, int) {
	exe, err := os.Executable()
	if err != nil {
		return false, 1
	}
	var argLine strings.Builder
	for i, arg := range args {
		if i > 0 {
			argLine.WriteByte(' ')
		}
		argLine.WriteString(quoteWindowsArgument(arg))
	}
	code, err := relaunchElevatedNative(exe, argLine.String())
	if err != nil {
		a.Log(context.Background(), "Elevation failed: "+err.Error(), "ERROR")
		return true, 1
	}
	return true, code
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

package app

import (
	"bufio"
	"context"
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
	DryRun             bool
	IncludeBios        bool
	LatestAcrossOS     bool
	CurrentOSOnly      bool
	TargetOS           string
	DownloadOnly       bool
	SkipHashCheck      bool
	SkipSignatureCheck bool
	Elevated           bool
	Help               bool
	Model              string
	DownloadDir        string
	GuiExportPath      string
	GuiInstallCodes    string
	Rollback           string
	AuditPath          string
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

	// driverListCacheDir is where the last-good driver list escape hatch is
	// persisted. It defaults to the stable audit artifact dir; tests override
	// it to t.TempDir() so cache writes never pollute the real directory.
	driverListCacheDir string
}

// artifactBaseDir returns the stable per-user directory for the operation log
// and install history. It prefers the LocalAppData cache root so the audit
// artifacts survive TEMP cleanup, falling back to TEMP when the cache root is
// unavailable. The directory is created eagerly so later appends never race a
// first-write MkdirAll.
func artifactBaseDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return os.TempDir()
	}
	dir := filepath.Join(base, "Lenovo", "DriverInstaller")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return os.TempDir()
	}
	return dir
}

// New returns a configured app. The log, history ledger, and the regenerated
// plan view all live under the stable per-user artifact directory so none of
// the three disappears on a TEMP cleanup. The plan is still a per-run view, not
// a persistent audit ledger: it is overwritten on every run and the WPF layer
// reads the same stable path instead of hardcoding %TEMP%.
func New(stdout, stderr io.Writer, stdin io.Reader) *App {
	base := artifactBaseDir()
	return &App{
		Stdout:      stdout,
		Stderr:      stderr,
		Stdin:       bufio.NewReader(stdin),
		LogPath:     filepath.Join(base, "lenovo_driver_install.log"),
		PlanPath:    filepath.Join(base, "lenovo_driver_plan.txt"),
		HistoryPath: filepath.Join(base, "lenovo_driver_history.csv"),
		APIClient:   api.NewClient(),
		Downloader:  download.NewDownloader(),
		startedAt:   time.Now(),

		driverListCacheDir: base,
	}
}

// Run executes the CLI and returns the process exit code.
func (a *App) Run(args []string) int {
	opts, err := parseOptions(args)
	if err != nil {
		fmt.Fprintln(a.Stderr, "Error: "+err.Error())
		return 2
	}
	if opts.Help {
		_, _ = io.WriteString(a.Stdout, helpText)
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

	if opts.Rollback != "" {
		return a.runRollback(ctx, opts.Rollback)
	}

	if opts.AuditPath != "" {
		return a.runAudit(ctx, opts.AuditPath)
	}

	vc, code := a.resolveRuntime(ctx, opts)
	if code != 0 {
		return code
	}

	listOsID, code := a.resolveListOS(ctx, vc, opts)
	if code != 0 {
		return code
	}

	view, err := a.CompareOSDriverView(ctx, vc, listOsID)
	if err != nil {
		a.Log(ctx, "Driver comparison failed: "+err.Error(), "ERROR")
		return 1
	}

	return a.runSelection(ctx, vc, view, listOsID)
}

// runSelection dispatches the export, dry-run, and select/install flows that
// follow a successful comparison view build, routing each run to a single
// named mode handler.
func (a *App) runSelection(ctx context.Context, vc *ViewContext, view *DriverView, listOsID string) int {
	switch {
	case vc.Opts.GuiExportPath != "":
		return a.runExport(ctx, vc, view)
	case vc.Opts.DryRun:
		a.Log(ctx, "Dry run finished. No files were downloaded or installed.", "INFO")
		return 0
	default:
		return a.runInstallFlow(ctx, vc, view, listOsID)
	}
}

// runExport writes the WPF-compatible JSON view and exits.
func (a *App) runExport(ctx context.Context, vc *ViewContext, view *DriverView) int {
	if err := a.ExportGUIView(vc.Opts.GuiExportPath, vc, view); err != nil {
		a.Log(ctx, "GUI export failed: "+err.Error(), "ERROR")
		return 1
	}
	a.Log(ctx, "GUI export : "+vc.Opts.GuiExportPath, "INFO")
	return 0
}

// runInstallFlow acquires the driver selection and then runs the bounded
// download/install/verify pass. The exit code from selection (0 for none, 3
// for a GUI-code mismatch) is returned unchanged.
func (a *App) runInstallFlow(ctx context.Context, vc *ViewContext, view *DriverView, listOsID string) int {
	selected, code := a.acquireSelection(ctx, vc, view, listOsID)
	if code != 0 {
		return code
	}
	if err := a.InstallSelected(ctx, vc, selected, view.Source, listOsID); err != nil {
		a.Log(ctx, "Install failed: "+err.Error(), "ERROR")
		return 1
	}
	return 0
}

// acquireSelection resolves the driver set to install from the GUI install
// codes or the interactive prompt. A nonzero code means the run should abort
// with that PS1-style exit code (3 for a GUI-code mismatch, 0 for a user
// cancel with nothing to install).
func (a *App) acquireSelection(ctx context.Context, vc *ViewContext, view *DriverView, listOsID string) ([]*model.AssessedDriver, int) {
	if vc.Opts.GuiInstallCodes != "" {
		selection := selectByCodes(view.Selected, vc.Opts.GuiInstallCodes)
		if len(selection.Missing) > 0 || len(selection.NotApplicable) > 0 || len(selection.Selected) == 0 {
			a.Log(ctx, "GUI-selected driver codes did not match the current list.", "ERROR")
			return nil, 3
		}
		return selection.Selected, 0
	}
	selection := a.SelectInteractive(ctx, vc, view, listOsID)
	if selection == nil {
		return nil, 0
	}
	return selection, 0
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

// resolveRuntime resolves the machine/OS identity and the Lenovo category and
// OS entry over the network while the local inventory snapshots run in
// parallel; the two legs join before the read-only view is built. The network
// leg decides the exit code (hard fail); the local leg keeps its
// degrade-to-empty policy.
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

	var (
		wg               sync.WaitGroup
		code             int
		categoryID       string
		osResolution     *api.OSResolution
		localDevices     []model.Device
		installedApps    []model.InstalledApp
		softwareSnapshot model.SoftwareSnapshot
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		categoryID, osResolution, code = a.resolveLenovoIdentity(ctx, opts, machine, osInfo)
	}()
	go func() {
		defer wg.Done()
		localDevices, installedApps, softwareSnapshot = a.localSnapshots(ctx)
	}()
	wg.Wait()
	if code != 0 {
		return nil, code
	}
	history := a.ReadHistory(ctx)

	return &ViewContext{
		Opts:              *opts,
		CategoryID:        categoryID,
		CurrentSystemOsID: osResolution.OSEntry.OSID,
		OsList:            osResolution.OSList,
		LocalDevices:      localDevices,
		InstalledApps:     installedApps,
		SoftwareSnapshot:  softwareSnapshot,
		History:           history,
		Machine:           machine,
		OSInfo:            osInfo,
	}, 0
}

// logOSResolution records which source supplied the OS list, and why the
// webpage list was not used when it was not. The cause is carried by the
// resolver rather than inferred here: "unavailable" was once printed for a
// decode failure, which sent the reader looking for a network problem that did
// not exist.
func (a *App) logOSResolution(ctx context.Context, res *api.OSResolution) {
	if res.Source == "Web" {
		return
	}
	a.Log(ctx, "OS list source : "+res.Source+"; webpage OS list not used: "+res.WebFailure, "WARN")
}

// resolveListOS resolves the OS identifier whose driver list will be compared
// against: the explicit -TargetOS entry, or the current system OS by default.
// It returns the PS1-style exit code (0 on success). The default OS entry is
// always a valid fallback, so it never fails on the normal path.
func (a *App) resolveListOS(ctx context.Context, vc *ViewContext, opts *Options) (string, int) {
	if opts.TargetOS != "" {
		target := compare.ResolveTargetOsEntry(vc.OsList, opts.TargetOS)
		if target == nil {
			a.Log(ctx, "Could not resolve -TargetOS '"+opts.TargetOS+"' from the Lenovo OS list for this machine.", "ERROR")
			return "", 1
		}
		a.Log(ctx, "Target OS entry : "+target.OSName+" (OSID "+target.OSID+")", "INFO")
		return target.OSID, 0
	}
	// The hint names a control only the interactive selection menu has. Export,
	// dry-run, and GUI-code runs never show that menu (the WPF switches OS with
	// a dropdown), so advertising "press t" there describes a key that cannot
	// do anything.
	if !opts.LatestAcrossOS && !opts.CurrentOSOnly && len(vc.OsList) > 1 &&
		!opts.DryRun && opts.GuiExportPath == "" && opts.GuiInstallCodes == "" {
		a.Log(ctx, "Current OS mode enabled (default). In the interactive menu, press t to switch supported OS lists.", "INFO")
	}
	return vc.CurrentSystemOsID, 0
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

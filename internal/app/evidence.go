package app

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lenovo-driver/internal/audit"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
	"lenovo-driver/internal/pathutil"
)

var importLogPaths = []string{
	`C:\Windows\INF\setupapi.offline.log`,
	`C:\Windows\INF\setupapi.dev.log`,
	`C:\Windows\INF\setupapi.setup.log`,
}

func (a *App) loadDriverImportRecords(ctx context.Context) []model.ImportRecord {
	var rows []model.ImportRecord
	for _, logPath := range importLogPaths {
		data, err := os.ReadFile(logPath)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		rows = append(rows, audit.ConvertFromImportLogText(lines, logPath)...)
	}
	return rows
}

// buildDeviceEvidenceMap reads PnP properties and attaches setupapi import
// evidence for a batch of matched devices in a single pass. The expensive
// evidence enumeration (one full SetupAPI scan) and the setupapi log reads
// happen once for all applicable drivers in a compare pass rather than once
// per driver; the caller then projects each driver's matched devices through
// this map. The map is keyed by PnP device id, and only contains rows that the
// evidence scan could resolve. A failed scan returns a nil map so the caller
// can degrade each driver to its raw matched rows exactly as a per-driver
// failure did before.
func (a *App) buildDeviceEvidenceMap(ctx context.Context, union []model.Device) (map[string]model.Device, error) {
	detailed, err := inventory.GetDeviceEvidence(ctx, union)
	if err != nil {
		return nil, err
	}
	imports := a.loadDriverImportRecords(ctx)
	byID := make(map[string]model.Device, len(detailed))
	for i := range detailed {
		device := &detailed[i]
		packageEvidence := resolvePackageEvidence(*device, imports)
		if packageEvidence != nil {
			device.PackageDir = packageEvidence.PackageDir
			device.PackageCreationTime = packageEvidence.PackageCreationTime
			device.PackageDriverVer = packageEvidence.PackageDriverVer
			device.ImportSource = packageEvidence.ImportSource
			device.ImportCommand = packageEvidence.ImportCommand
			device.ImportLog = packageEvidence.ImportLog
			device.ImportLine = packageEvidence.ImportLine
			device.ImportKind = packageEvidence.ImportKind
		}
		byID[device.PnpDeviceID] = *device
	}
	return byID, nil
}

// projectMatchedEvidence returns the enriched evidence rows for the matched
// devices that the evidence scan found, in matched order. When the evidence
// map is nil (the evidence pass failed), it returns the plain matched rows so
// the source audit still degrades gracefully, preserving the pre-batch
// behavior of falling back to the unmatched input.
func projectMatchedEvidence(matched []model.Device, byID map[string]model.Device) []model.Device {
	if byID == nil {
		return matched
	}
	out := make([]model.Device, 0, len(matched))
	for _, device := range matched {
		if enriched, ok := byID[device.PnpDeviceID]; ok {
			out = append(out, enriched)
		}
	}
	return out
}

type packageEvidence struct {
	PackageDir          string
	PackageCreationTime string
	PackageDriverVer    string
	ImportSource        string
	ImportCommand       string
	ImportLog           string
	ImportLine          int
	ImportKind          string
}

func resolvePackageEvidence(device model.Device, imports []model.ImportRecord) *packageEvidence {
	infLeaf := pathutil.Base(device.InfName)
	infParent := pathutil.Parent(device.InfName)
	packageDirFromInf := ""
	if strings.Contains(infParent, `DriverStore\FileRepository`) {
		packageDirFromInf = infParent
	}
	var matching []model.ImportRecord
	for _, row := range imports {
		rowInf := pathutil.Base(row.InfName)
		rowOem := pathutil.Base(row.OemInfName)
		rowDir := pathutil.Base(row.PackageDir)
		if (rowInf != "" && strings.EqualFold(rowInf, infLeaf)) ||
			(rowOem != "" && strings.EqualFold(rowOem, infLeaf)) ||
			(packageDirFromInf != "" && rowDir != "" && strings.EqualFold(rowDir, pathutil.Base(packageDirFromInf))) {
			matching = append(matching, row)
		}
	}
	var importRow *model.ImportRecord
	for _, row := range matching {
		if row.Version == device.DriverVersion || row.Version == "" {
			candidate := row
			if importRow == nil || candidate.LineNumber > importRow.LineNumber {
				importRow = &candidate
			}
		}
	}
	if importRow == nil && len(matching) > 0 {
		sort.SliceStable(matching, func(i, j int) bool { return matching[i].LineNumber > matching[j].LineNumber })
		candidate := matching[0]
		importRow = &candidate
	}

	packageDir := ""
	packageCreation := ""
	if importRow != nil && importRow.PackageDir != "" {
		baseName := pathutil.Base(importRow.PackageDir)
		candidate := filepath.Join(`C:\Windows\System32\DriverStore\FileRepository`, baseName)
		if info, err := os.Stat(candidate); err == nil {
			packageDir = candidate
			packageCreation = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	if packageDir == "" && packageDirFromInf != "" {
		packageDir = packageDirFromInf
		if info, err := os.Stat(packageDir); err == nil {
			packageCreation = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	if importRow == nil {
		return nil
	}
	kind := "Online"
	if strings.Contains(strings.ToLower(importRow.LogPath), "offline") {
		kind = "Offline"
	}
	return &packageEvidence{
		PackageDir:          packageDir,
		PackageCreationTime: packageCreation,
		PackageDriverVer:    importRow.Version,
		ImportSource:        importRow.SourcePath,
		ImportCommand:       importRow.Command,
		ImportLog:           importRow.LogPath,
		ImportLine:          importRow.LineNumber,
		ImportKind:          kind,
	}
}

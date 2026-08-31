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

// enrichDeviceEvidence reads PnP properties and attaches setupapi import evidence.
func (a *App) enrichDeviceEvidence(ctx context.Context, matched []model.Device) []model.Device {
	detailed, err := inventory.GetDeviceEvidence(ctx, matched)
	if err != nil {
		a.Log(ctx, "Device evidence lookup failed: "+err.Error(), "WARN")
		return matched
	}
	imports := a.loadDriverImportRecords(ctx)
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
	}
	return detailed
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

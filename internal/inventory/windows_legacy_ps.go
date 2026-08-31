//go:build windows && legacyps

package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"lenovo-driver/internal/model"
)

// Legacy PowerShell inventory is retained behind the `legacyps` build tag only
// as a comparison oracle for real-machine equivalence tests. It is not part of
// the normal runtime path.

// powershellExe is the stable Windows PowerShell path used by the legacy
// oracle only; the runtime path is native-only.
const powershellExe = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

func runLegacyJSON(ctx context.Context, script string, target any) error {
	cmd := exec.CommandContext(ctx, powershellExe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("legacy PowerShell inventory failed: %w: %s", err, detail)
		}
		return fmt.Errorf("legacy PowerShell inventory failed: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}
	return json.Unmarshal(out, target)
}

// GetMachineInfoPS is the legacy WMI/CIM oracle.
func GetMachineInfoPS(ctx context.Context) (MachineInfo, error) {
	var info MachineInfo
	script := `$cs = Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue; if (-not $cs) { $cs = Get-WmiObject Win32_ComputerSystem }; $bios = Get-CimInstance Win32_BIOS -ErrorAction SilentlyContinue; if (-not $bios) { $bios = Get-WmiObject Win32_BIOS }; [pscustomobject]@{ Model = [string]$cs.Model; Serial = [string]$bios.SerialNumber } | ConvertTo-Json -Compress`
	if err := runLegacyJSON(ctx, script, &info); err != nil {
		return info, err
	}
	return info, nil
}

// GetOSInfoPS is the legacy WMI/CIM oracle.
func GetOSInfoPS(ctx context.Context) (OSInfo, error) {
	var raw osInfoRow
	script := `$os = Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue; if (-not $os) { $os = Get-WmiObject Win32_OperatingSystem }; $os | Select-Object Caption,OSArchitecture | ConvertTo-Json -Compress`
	if err := runLegacyJSON(ctx, script, &raw); err != nil {
		return OSInfo{}, err
	}
	return normalizeOSInfo(raw), nil
}

// GetLocalDeviceSnapshotPS is the legacy WMI/PnP oracle.
func GetLocalDeviceSnapshotPS(ctx context.Context) ([]model.Device, error) {
	var rows []struct {
		Name        string `json:"Name"`
		PNPClass    string `json:"PNPClass"`
		DeviceID    string `json:"DeviceID"`
		PNPDeviceID string `json:"PNPDeviceID"`
	}
	script := `$entities = @(Get-CimInstance Win32_PnPEntity -ErrorAction SilentlyContinue); if (-not $entities) { $entities = @(Get-WmiObject Win32_PnPEntity) }; $entities | Where-Object { $_.Present -eq $true } | Select-Object Name,PNPClass,DeviceID,PNPDeviceID | ConvertTo-Json -Compress -Depth 3`
	if err := runLegacyJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	devices := make([]model.Device, 0, len(rows))
	for _, row := range rows {
		devices = append(devices, model.Device{
			Name:        row.Name,
			Class:       row.PNPClass,
			DeviceID:    row.DeviceID,
			PnpDeviceID: row.PNPDeviceID,
		})
	}
	return devices, nil
}

// GetInstalledAppsPS is the legacy registry oracle.
func GetInstalledAppsPS(ctx context.Context) ([]model.InstalledApp, error) {
	var rows []struct {
		DisplayName    string `json:"DisplayName"`
		DisplayVersion string `json:"DisplayVersion"`
	}
	script := `$paths = @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*'); $apps = @(); foreach ($path in $paths) { try { $apps += @(Get-ItemProperty -Path $path -ErrorAction Stop | Where-Object { $_.DisplayName }) } catch {} }; $apps | Select-Object DisplayName,DisplayVersion | ConvertTo-Json -Compress -Depth 3`
	if err := runLegacyJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	apps := make([]model.InstalledApp, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, model.InstalledApp{DisplayName: row.DisplayName, DisplayVersion: row.DisplayVersion})
	}
	return apps, nil
}

// GetSoftwareSnapshotPS is the legacy provisioning/service oracle.
func GetSoftwareSnapshotPS(ctx context.Context, installedApps []model.InstalledApp) (model.SoftwareSnapshot, error) {
	snapshot := model.SoftwareSnapshot{InstalledApps: installedApps}
	var raw struct {
		ProvisionedAmdPower string `json:"ProvisionedAmdPower"`
		FnServiceVersion    string `json:"FnServiceVersion"`
	}
	script := `$provisioned = ''; try { $provisioned = @(Get-ChildItem 'HKLM:\SOFTWARE\Microsoft\Provisioning\Results' -ErrorAction Stop | ForEach-Object { Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue } | Where-Object { $_.PackageFileName -eq 'AMD.Power.Processor.ppkg' }).Count; if ($provisioned -gt 0) { $provisioned = 'Provisioned' } else { $provisioned = '' } } catch {}; $fnServiceVersion = ''; try { $svc = Get-CimInstance Win32_Service -Filter "Name='LenovoFnAndFunctionKeys'" -ErrorAction SilentlyContinue; if ($svc) { $exePath = ([string]$svc.PathName).Trim('"'); if ($exePath -and (Test-Path -LiteralPath $exePath)) { $fnServiceVersion = [string](Get-Item -LiteralPath $exePath).VersionInfo.FileVersion } } } catch {}; [pscustomobject]@{ ProvisionedAmdPower = $provisioned; FnServiceVersion = $fnServiceVersion } | ConvertTo-Json -Compress`
	if err := runLegacyJSON(ctx, script, &raw); err != nil {
		return snapshot, err
	}
	snapshot.ProvisionedAmdPower = raw.ProvisionedAmdPower
	snapshot.LenovoFnServiceVersion = raw.FnServiceVersion
	return snapshot, nil
}

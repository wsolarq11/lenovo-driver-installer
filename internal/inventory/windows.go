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

// PowerShellExe is the stable Windows PowerShell path used by inventory queries.
const PowerShellExe = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

// MachineInfo is the resolved machine identity.
type MachineInfo struct {
	Model  string
	Serial string
}

// OSInfo is the resolved local OS identity.
type OSInfo struct {
	Caption string
	Kind    string
	OSName  string
	Arch    string
}

type osInfoRow struct {
	Caption        string `json:"Caption"`
	OSArchitecture string `json:"OSArchitecture"`
}

func runJSON(ctx context.Context, script string, target any) error {
	cmd := exec.CommandContext(ctx, PowerShellExe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("powerShell inventory failed: %w: %s", err, detail)
		}
		return fmt.Errorf("powerShell inventory failed: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}
	return json.Unmarshal(out, target)
}

// GetMachineInfo resolves model and serial through CIM/WMI.
func GetMachineInfo(ctx context.Context) (MachineInfo, error) {
	var info MachineInfo
	script := `$cs = Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue; if (-not $cs) { $cs = Get-WmiObject Win32_ComputerSystem }; $bios = Get-CimInstance Win32_BIOS -ErrorAction SilentlyContinue; if (-not $bios) { $bios = Get-WmiObject Win32_BIOS }; [pscustomobject]@{ Model = [string]$cs.Model; Serial = [string]$bios.SerialNumber } | ConvertTo-Json -Compress`
	if err := runJSON(ctx, script, &info); err != nil {
		return info, err
	}
	return info, nil
}

// GetOSInfo resolves caption, kind, architecture, and the Lenovo OS match key.
func GetOSInfo(ctx context.Context) (OSInfo, error) {
	var raw osInfoRow
	script := `$os = Get-CimInstance Win32_OperatingSystem -ErrorAction SilentlyContinue; if (-not $os) { $os = Get-WmiObject Win32_OperatingSystem }; $os | Select-Object Caption,OSArchitecture | ConvertTo-Json -Compress`
	if err := runJSON(ctx, script, &raw); err != nil {
		return OSInfo{}, err
	}
	return normalizeOSInfo(raw), nil
}

func normalizeOSInfo(raw osInfoRow) OSInfo {
	kind := "Windows"
	caption := raw.Caption
	switch {
	case strings.Contains(caption, "Windows 11"):
		kind = "Windows 11"
	case strings.Contains(caption, "Windows 10"):
		kind = "Windows 10"
	case strings.Contains(caption, "Windows 8"):
		kind = "Windows 8"
	case strings.Contains(caption, "Windows 7"):
		kind = "Windows 7"
	}
	bits := "32-bit"
	if strings.Contains(raw.OSArchitecture, "64") {
		bits = "64-bit"
	}
	return OSInfo{
		Caption: caption,
		Kind:    kind,
		OSName:  kind + " " + bits,
		Arch:    bits,
	}
}

// GetLocalDeviceSnapshot returns present PnP devices.
func GetLocalDeviceSnapshot(ctx context.Context) ([]model.Device, error) {
	var rows []struct {
		Name        string `json:"Name"`
		PNPClass    string `json:"PNPClass"`
		DeviceID    string `json:"DeviceID"`
		PNPDeviceID string `json:"PNPDeviceID"`
	}
	script := `$entities = @(Get-CimInstance Win32_PnPEntity -ErrorAction SilentlyContinue); if (-not $entities) { $entities = @(Get-WmiObject Win32_PnPEntity) }; $entities | Where-Object { $_.Present -eq $true } | Select-Object Name,PNPClass,DeviceID,PNPDeviceID | ConvertTo-Json -Compress -Depth 3`
	if err := runJSON(ctx, script, &rows); err != nil {
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

// GetInstalledApps reads uninstall registry entries as installed application evidence.
func GetInstalledApps(ctx context.Context) ([]model.InstalledApp, error) {
	var rows []struct {
		DisplayName    string `json:"DisplayName"`
		DisplayVersion string `json:"DisplayVersion"`
	}
	script := `$paths = @('HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*'); $apps = @(); foreach ($path in $paths) { try { $apps += @(Get-ItemProperty -Path $path -ErrorAction Stop | Where-Object { $_.DisplayName }) } catch {} }; $apps | Select-Object DisplayName,DisplayVersion | ConvertTo-Json -Compress -Depth 3`
	if err := runJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	apps := make([]model.InstalledApp, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, model.InstalledApp{DisplayName: row.DisplayName, DisplayVersion: row.DisplayVersion})
	}
	return apps, nil
}

// GetSoftwareSnapshot resolves software-only driver evidence.
func GetSoftwareSnapshot(ctx context.Context, installedApps []model.InstalledApp) (model.SoftwareSnapshot, error) {
	snapshot := model.SoftwareSnapshot{InstalledApps: installedApps}
	var raw struct {
		ProvisionedAmdPower string `json:"ProvisionedAmdPower"`
		FnServiceVersion    string `json:"FnServiceVersion"`
	}
	script := `$provisioned = ''; try { $provisioned = @(Get-ChildItem 'HKLM:\SOFTWARE\Microsoft\Provisioning\Results' -ErrorAction Stop | ForEach-Object { Get-ItemProperty $_.PSPath -ErrorAction SilentlyContinue } | Where-Object { $_.PackageFileName -eq 'AMD.Power.Processor.ppkg' }).Count; if ($provisioned -gt 0) { $provisioned = 'Provisioned' } else { $provisioned = '' } } catch {}; $fnServiceVersion = ''; try { $svc = Get-CimInstance Win32_Service -Filter "Name='LenovoFnAndFunctionKeys'" -ErrorAction SilentlyContinue; if ($svc) { $exePath = ([string]$svc.PathName).Trim('"'); if ($exePath -and (Test-Path -LiteralPath $exePath)) { $fnServiceVersion = [string](Get-Item -LiteralPath $exePath).VersionInfo.FileVersion } } } catch {}; [pscustomobject]@{ ProvisionedAmdPower = $provisioned; FnServiceVersion = $fnServiceVersion } | ConvertTo-Json -Compress`
	if err := runJSON(ctx, script, &raw); err != nil {
		return snapshot, err
	}
	snapshot.ProvisionedAmdPower = raw.ProvisionedAmdPower
	snapshot.LenovoFnServiceVersion = raw.FnServiceVersion
	return snapshot, nil
}

// quotePnpIDs returns the ids as an SQL-style single-quoted CSV list for
// embedding in a PowerShell array literal, doubling embedded single quotes.
func quotePnpIDs(ids []string) []string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, "'"+strings.ReplaceAll(id, "'", "''")+"'")
	}
	return quoted
}

// GetDeviceDriverVersions reads DEVPKEY_Device_DriverVersion for matching PnP devices.
func GetDeviceDriverVersions(ctx context.Context, pnpIDs []string) ([]string, error) {
	if len(pnpIDs) == 0 {
		return nil, nil
	}
	var versions []string
	script := `$ids = @(` + strings.Join(quotePnpIDs(pnpIDs), ",") + `); $result = @(); foreach ($id in $ids) { try { $p = Get-PnpDeviceProperty -InstanceId $id -KeyName 'DEVPKEY_Device_DriverVersion' -ErrorAction Stop; if ($p.Data) { $result += [string]$p.Data } } catch {} }; $result | ConvertTo-Json -Compress`
	if err := runJSON(ctx, script, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// GetDeviceEvidence reads PnP driver properties for matched devices.
func GetDeviceEvidence(ctx context.Context, devices []model.Device) ([]model.Device, error) {
	ids := make([]string, 0, len(devices))
	for _, dev := range devices {
		ids = append(ids, dev.PnpDeviceID)
	}
	quoted := quotePnpIDs(ids)
	if len(quoted) == 0 {
		return nil, nil
	}
	var rows []struct {
		PnpDeviceID   string `json:"PnpDeviceID"`
		Name          string `json:"Name"`
		Class         string `json:"Class"`
		DriverVersion string `json:"DriverVersion"`
		DriverDate    string `json:"DriverDate"`
		InfName       string `json:"InfName"`
		ProviderName  string `json:"ProviderName"`
		InstallDate   string `json:"InstallDate"`
	}
	keys := `'DEVPKEY_Device_DriverVersion','DEVPKEY_Device_DriverDate','DEVPKEY_Device_DriverInfPath','DEVPKEY_Device_DriverProvider','DEVPKEY_Device_InstallDate'`
	script := `$ids = @(` + strings.Join(quoted, ",") + `); $out = @(); foreach ($id in $ids) { try { $dev = Get-PnpDevice -InstanceId $id -ErrorAction Stop; $props = @{}; foreach ($key in @(` + keys + `)) { try { $p = Get-PnpDeviceProperty -InstanceId $id -KeyName $key -ErrorAction Stop; if ($p.Data) { $props[$key] = [string]$p.Data } } catch {} }; $out += [pscustomobject]@{ PnpDeviceID = $id; Name = [string]$dev.FriendlyName; Class = [string]$dev.Class; DriverVersion = [string]$props['DEVPKEY_Device_DriverVersion']; DriverDate = [string]$props['DEVPKEY_Device_DriverDate']; InfName = [string]$props['DEVPKEY_Device_DriverInfPath']; ProviderName = [string]$props['DEVPKEY_Device_DriverProvider']; InstallDate = [string]$props['DEVPKEY_Device_InstallDate'] } } catch {} }; $out | ConvertTo-Json -Compress -Depth 3`
	if err := runJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	byID := map[string]model.Device{}
	for _, dev := range devices {
		byID[dev.PnpDeviceID] = dev
	}
	var result []model.Device
	for _, row := range rows {
		base := byID[row.PnpDeviceID]
		base.Name = row.Name
		base.Class = row.Class
		base.DriverVersion = row.DriverVersion
		base.DriverDate = row.DriverDate
		base.InfName = row.InfName
		base.ProviderName = row.ProviderName
		base.InstallDate = row.InstallDate
		result = append(result, base)
	}
	return result, nil
}

// IsAdministrator reports whether the current token is elevated.
func IsAdministrator() (bool, error) {
	cmd := exec.Command(PowerShellExe, "-NoProfile", "-NonInteractive", "-Command", `$p=[Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent(); if ($p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { '1' } else { '0' }`)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "1"), nil
}

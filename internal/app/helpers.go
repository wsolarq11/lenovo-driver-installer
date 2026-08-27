package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/inventory"
	"lenovo-driver/internal/model"
)

var logMu sync.Mutex

// Log writes one line to TEMP log and mirrors it to stdout.
func (a *App) Log(ctx context.Context, message, level string) {
	logMu.Lock()
	defer logMu.Unlock()
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s", ts, level, message)
	fmt.Fprintln(a.Stdout, line)
	if err := appendLine(a.LogPath, line); err != nil && a.Stderr != nil {
		fmt.Fprintf(a.Stderr, "WARN failed to append log file %s: %s\n", a.LogPath, err)
	}
}

func appendLine(path, line string) error {
	f, err := openAppend(path)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\r\n")
	if err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func execPowershell(script string) *exec.Cmd {
	cmd := exec.Command(inventory.PowerShellExe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	return cmd
}

func resolveTarget(osList []model.OSListEntry, targetOS string) *model.OSListEntry {
	return compare.ResolveTargetOsEntry(osList, targetOS)
}

func selectByCodes(drivers []*model.Driver, codes string) []*model.Driver {
	codeSet := map[string]bool{}
	for _, code := range strings.Split(codes, ",") {
		code = strings.TrimSpace(code)
		if code != "" {
			codeSet[code] = true
		}
	}
	var selected []*model.Driver
	for _, driver := range drivers {
		if driver != nil && codeSet[driver.DriverCode] {
			selected = append(selected, driver)
		}
	}
	return selected
}

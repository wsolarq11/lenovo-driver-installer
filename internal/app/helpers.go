package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"lenovo-driver/internal/model"
)

var logMu sync.Mutex

// formatTimestamp renders a time in UTC as RFC3339 (with seconds). Using the
// RFC3339 wire format keeps every timestamp self-describing and lexically
// sortable, matching the AGENTS.md time-unification rule (UTC+0, RFC3339).
func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Log writes one line to TEMP log and mirrors it to stdout.
func (a *App) Log(ctx context.Context, message, level string) {
	logMu.Lock()
	defer logMu.Unlock()
	ts := formatTimestamp(time.Now())
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

type codeSelection struct {
	Selected      []*model.AssessedDriver
	Missing       []string
	NotApplicable []string
}

func quoteWindowsArgument(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\n\r\v\f\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		if r == '\\' {
			backslashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat(`\`, backslashes*2))
			backslashes = 0
			b.WriteString(`\"`)
			continue
		}
		b.WriteString(strings.Repeat(`\`, backslashes))
		backslashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, backslashes*2))
	b.WriteByte('"')
	return b.String()
}

func selectByCodes(drivers []*model.AssessedDriver, codes string) codeSelection {
	requested := map[string]bool{}
	for _, token := range strings.Split(codes, ",") {
		code := strings.TrimSpace(token)
		if code != "" {
			requested[code] = true
		}
	}
	var selection codeSelection
	// One pass over drivers in list order keeps Selected in the same order as
	// the caller sees them, while a requested set gives O(1) membership and
	// missing lookups (no nested rescans).
	for _, ad := range drivers {
		if ad == nil || ad.Driver == nil || !requested[ad.DriverCode] {
			continue
		}
		requested[ad.DriverCode] = false
		if ad.CompareStatus == model.StatusNotApplicable {
			selection.NotApplicable = append(selection.NotApplicable, ad.DriverCode)
			continue
		}
		selection.Selected = append(selection.Selected, ad)
	}
	for code, wanted := range requested {
		if wanted {
			selection.Missing = append(selection.Missing, code)
		}
	}
	sort.Strings(selection.Missing)
	sort.Strings(selection.NotApplicable)
	return selection
}

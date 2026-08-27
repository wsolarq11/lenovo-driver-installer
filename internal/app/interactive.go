package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"lenovo-driver/internal/model"
	"lenovo-driver/internal/plan"
)

// SelectionAnswer is the parsed interactive answer.
type SelectionAnswer struct {
	Drivers []*model.Driver
	Toggle  bool
	Cancel  bool
}

// SelectInteractive mirrors Select-InteractiveDrivers, reloading the view on t.
func (a *App) SelectInteractive(
	ctx context.Context,
	opts *Options,
	view *DriverView,
	categoryID string,
	listOsID string,
	sysID string,
	osList []model.OSListEntry,
	localDevices []model.Device,
	installedApps []model.InstalledApp,
	softwareSnapshot model.SoftwareSnapshot,
	history []model.HistoryRecord,
) []*model.Driver {
	currentView := view
	currentListOsID := listOsID
	for {
		allowToggle := !opts.CurrentOSOnly && !opts.LatestAcrossOS && !opts.TargetOSActive()
		nextLabel := nextOSLabel(osList, currentListOsID)
		answer := a.promptSelection(currentView, allowToggle, nextLabel)
		if answer.Toggle {
			nextIndex := nextOSIndex(osList, currentListOsID)
			if nextIndex < 0 {
				a.Log(ctx, "No alternate OS list available.", "WARN")
				continue
			}
			nextListOsID := osList[nextIndex].OSID
			nextView, err := a.CompareOSDriverView(ctx, opts, categoryID, nextListOsID, sysID, osList, localDevices, installedApps, softwareSnapshot, history)
			if err != nil {
				a.Log(ctx, "Could not load alternate OS list: "+err.Error(), "WARN")
				continue
			}
			currentListOsID = nextListOsID
			currentView = nextView
			continue
		}
		if answer.Cancel {
			a.Log(ctx, "No drivers were selected.", "INFO")
			return nil
		}
		if len(answer.Drivers) == 0 {
			a.Log(ctx, "No drivers selected for installation.", "INFO")
			return nil
		}
		return answer.Drivers
	}
}

func (a *App) promptSelection(view *DriverView, allowToggle bool, nextOSLabel string) SelectionAnswer {
	updates := view.Updates
	applicable := view.Applicable
	if len(updates) > 0 {
		fmt.Fprintf(a.Stdout, "Ready: %d update-only drivers, %d all applicable drivers.\n", len(updates), len(applicable))
		showActionPreview(a.Stdout, 'y', "update-only", updates)
		showActionPreview(a.Stdout, 'a', "all applicable", applicable)
		toggleText := ""
		if allowToggle {
			toggleText = ", t to switch to " + nextOSLabel
		}
		fmt.Fprintf(a.Stdout, "Type y to install the update-only set, a to install the all-applicable set, s to select%s, n to cancel: ", toggleText)
		choice := strings.ToLower(strings.TrimSpace(readLine(a.Stdin)))
		if choice == "t" && allowToggle {
			return SelectionAnswer{Toggle: true}
		}
		if choice == "y" {
			return SelectionAnswer{Drivers: updates}
		}
		if choice == "a" {
			return SelectionAnswer{Drivers: applicable}
		}
		if choice == "s" {
			manual := readDriverSelection(a.Stdin, a.Stdout, view)
			if len(manual) == 0 {
				a.Log(context.Background(), "No drivers selected.", "INFO")
				return SelectionAnswer{Cancel: true}
			}
			return SelectionAnswer{Drivers: manual}
		}
		return SelectionAnswer{Cancel: true}
	}

	fmt.Fprintf(a.Stdout, "No clear updates detected. %d applicable candidates remain.\n", len(applicable))
	showActionPreview(a.Stdout, 'a', "all applicable", applicable)
	toggleText := ""
	if allowToggle {
		toggleText = ", t to switch to " + nextOSLabel
	}
	fmt.Fprintf(a.Stdout, "Type a to install the all-applicable set, s to select%s, n to cancel: ", toggleText)
	choice := strings.ToLower(strings.TrimSpace(readLine(a.Stdin)))
	if choice == "t" && allowToggle {
		return SelectionAnswer{Toggle: true}
	}
	if choice == "a" {
		return SelectionAnswer{Drivers: applicable}
	}
	if choice == "s" {
		manual := readDriverSelection(a.Stdin, a.Stdout, view)
		if len(manual) == 0 {
			a.Log(context.Background(), "No drivers selected.", "INFO")
			return SelectionAnswer{Cancel: true}
		}
		return SelectionAnswer{Drivers: manual}
	}
	return SelectionAnswer{Cancel: true}
}

func showActionPreview(w io.Writer, key rune, label string, drivers []*model.Driver) {
	fmt.Fprintf(w, "  %c = %s (%d)\n", key, label, len(drivers))
	for _, line := range plan.FormatDriverTableLines(drivers) {
		fmt.Fprintln(w, line)
	}
}

func readLine(reader *bufio.Reader) string {
	line, _ := reader.ReadString('\n')
	return line
}

func nextOSIndex(osList []model.OSListEntry, currentID string) int {
	start := -1
	for i, entry := range osList {
		if entry.OSID == currentID {
			start = i
			break
		}
	}
	if start < 0 {
		return -1
	}
	for i := 1; i <= len(osList); i++ {
		idx := (start + i) % len(osList)
		if osList[idx].OSID != currentID {
			return idx
		}
	}
	return -1
}

func nextOSLabel(osList []model.OSListEntry, currentID string) string {
	idx := nextOSIndex(osList, currentID)
	if idx < 0 || idx >= len(osList) {
		return ""
	}
	return osList[idx].OSName + " (OSID " + osList[idx].OSID + ")"
}

func readDriverSelection(reader *bufio.Reader, writer io.Writer, view *DriverView) []*model.Driver {
	if view == nil || len(view.Selected) == 0 {
		return nil
	}
	fmt.Fprintln(writer, "Enter driver numbers separated by commas (for example: 1,3,5):")
	tokens := readLine(reader)
	result := plan.ParseDriverSelectionTokens(tokens, view.Selected)
	for _, token := range result.Invalid {
		fmt.Fprintf(writer, "Invalid selection: %s\n", token)
	}
	for _, token := range result.NotApplicable {
		fmt.Fprintf(writer, "Not applicable: %s\n", token)
	}
	return result.Selected
}

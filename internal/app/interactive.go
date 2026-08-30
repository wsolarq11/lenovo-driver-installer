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
	vc *ViewContext,
	view *DriverView,
	listOsID string,
) []*model.Driver {
	currentView := view
	currentListOsID := listOsID
	for {
		allowToggle := !vc.Opts.CurrentOSOnly && !vc.Opts.LatestAcrossOS && !vc.Opts.TargetOSActive()
		nextLabel := nextOSLabel(vc.OsList, currentListOsID)
		answer := a.promptSelection(currentView, allowToggle, nextLabel)
		if answer.Toggle {
			nextIndex := nextOSIndex(vc.OsList, currentListOsID)
			if nextIndex < 0 {
				a.Log(ctx, "No alternate OS list available.", "WARN")
				continue
			}
			nextListOsID := vc.OsList[nextIndex].OSID
			nextView, err := a.CompareOSDriverView(ctx, vc, nextListOsID)
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
	if len(view.Updates) > 0 {
		fmt.Fprintf(a.Stdout, "Ready: %d update-only drivers, %d all applicable drivers.\n", len(view.Updates), len(view.Applicable))
		showActionPreview(a.Stdout, 'y', "update-only", view.Updates)
		showActionPreview(a.Stdout, 'a', "all applicable", view.Applicable)
		return a.promptChoice(view, allowToggle, nextOSLabel, true)
	}

	fmt.Fprintf(a.Stdout, "No clear updates detected. %d applicable candidates remain.\n", len(view.Applicable))
	showActionPreview(a.Stdout, 'a', "all applicable", view.Applicable)
	return a.promptChoice(view, allowToggle, nextOSLabel, false)
}

func (a *App) promptChoice(view *DriverView, allowToggle bool, nextOSLabel string, hasUpdates bool) SelectionAnswer {
	toggleText := ""
	if allowToggle {
		toggleText = ", t to switch to " + nextOSLabel
	}
	if hasUpdates {
		fmt.Fprintf(a.Stdout, "Type y to install the update-only set, a to install the all-applicable set, s to select%s, n to cancel: ", toggleText)
	} else {
		fmt.Fprintf(a.Stdout, "Type a to install the all-applicable set, s to select%s, n to cancel: ", toggleText)
	}
	line, err := readLine(a.Stdin)
	if err != nil {
		return SelectionAnswer{Cancel: true}
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	if choice == "t" && allowToggle {
		return SelectionAnswer{Toggle: true}
	}
	if choice == "y" && hasUpdates {
		return SelectionAnswer{Drivers: view.Updates}
	}
	if choice == "a" {
		return SelectionAnswer{Drivers: view.Applicable}
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

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if len(line) == 0 && err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
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
	tokens, err := readLine(reader)
	if err != nil {
		return nil
	}
	result := plan.ParseDriverSelectionTokens(tokens, view.Selected)
	for _, token := range result.Invalid {
		fmt.Fprintf(writer, "Invalid selection: %s\n", token)
	}
	for _, token := range result.NotApplicable {
		fmt.Fprintf(writer, "Not applicable: %s\n", token)
	}
	return result.Selected
}

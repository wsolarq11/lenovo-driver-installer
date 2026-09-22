package app

import (
	"reflect"
	"strings"
	"testing"
)

// TestGUIExportJSONFields locks the WPF JSON field set. docs/spec/contracts.md
// carries a human-readable copy of these struct tags; adding, renaming, or
// removing a field must update the doc and this golden together so the two
// cannot drift silently.
func TestGUIExportJSONFields(t *testing.T) {
	payload := jsonFieldNames(t, reflect.TypeOf(guiExportPayload{}))
	wantPayload := []string{
		"GeneratedAt", "MachineModel", "SerialNumber", "SystemCaption",
		"CurrentOsId", "CurrentOsName", "ListOsId", "ListOsName",
		"DataSource", "OsList", "Drivers",
	}
	if !reflect.DeepEqual(payload, wantPayload) {
		t.Fatalf("guiExportPayload fields drift:\n got %v\nwant %v", payload, wantPayload)
	}

	row := jsonFieldNames(t, reflect.TypeOf(guiDriverRow{}))
	wantRow := []string{
		"Selected", "DriverCode", "DriverName", "Version", "LocalVersion",
		"CompareStatus", "SourceAudit", "CompareSource", "DeviceProblem",
		"EvidenceBasis", "NonMatchReason", "FileName", "FilePath", "FileSize",
		"MD5", "IsApplicable", "IsUpdate",
	}
	if !reflect.DeepEqual(row, wantRow) {
		t.Fatalf("guiDriverRow fields drift:\n got %v\nwant %v", row, wantRow)
	}
}

// jsonFieldNames returns the JSON keys of a struct in declaration order,
// stripping options such as ",omitempty".
func jsonFieldNames(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		if idx := strings.IndexByte(name, ','); idx >= 0 {
			name = name[:idx]
		}
		names = append(names, name)
	}
	return names
}

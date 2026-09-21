package install

import (
	"reflect"
	"strings"
	"testing"

	"lenovo-driver/internal/model"
)

func TestDeleteDriverArgs(t *testing.T) {
	got := deleteDriverArgs("oem42.inf")
	want := []string{"/delete-driver", "oem42.inf", "/uninstall", "/force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deleteDriverArgs = %#v, want %#v", got, want)
	}
}

func TestRollbackErrorClassification(t *testing.T) {
	noBackup := &RollbackError{Code: 259}
	if !noBackup.NoBackup() {
		t.Fatalf("ERROR_NO_MORE_ITEMS must report NoBackup, error: %q", noBackup.Error())
	}
	if noBackup.Error() != "no backup driver available to roll back" {
		t.Fatalf("no-backup error text = %q", noBackup.Error())
	}

	access := &RollbackError{Code: 5}
	if access.NoBackup() {
		t.Fatalf("ERROR_ACCESS_DENIED must not report NoBackup")
	}
	if !strings.Contains(access.Error(), "administrator privileges") {
		t.Fatalf("access-denied error text = %q", access.Error())
	}

	wow := &RollbackError{Code: 129}
	if wow.NoBackup() {
		t.Fatalf("ERROR_IN_WOW64 must not report NoBackup")
	}
	if !strings.Contains(wow.Error(), "32-bit") {
		t.Fatalf("wow64 error text = %q", wow.Error())
	}

	unknown := &RollbackError{Code: 999}
	if unknown.NoBackup() {
		t.Fatalf("unknown code must not report NoBackup")
	}
	if !strings.Contains(unknown.Error(), "999") {
		t.Fatalf("unknown error text = %q", unknown.Error())
	}
}

func TestRollbackDeviceIDs(t *testing.T) {
	devices := []model.Device{
		{PnpDeviceID: "A", ProblemNumber: 0},
		{PnpDeviceID: "B", ProblemNumber: 43},
		{PnpDeviceID: "", ProblemNumber: 43},
		{PnpDeviceID: "D", ProblemNumber: 28},
	}
	got := RollbackDeviceIDs(devices)
	want := []string{"B", "D"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RollbackDeviceIDs = %#v, want %#v", got, want)
	}
}

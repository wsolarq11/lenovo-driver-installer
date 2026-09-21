package install

import (
	"reflect"
	"testing"
)

func TestDeleteDriverArgs(t *testing.T) {
	got := deleteDriverArgs("oem42.inf")
	want := []string{"/delete-driver", "oem42.inf", "/uninstall", "/force"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deleteDriverArgs = %#v, want %#v", got, want)
	}
}

//go:build windows

package inventory

import (
	"testing"

	"lenovo-driver/internal/model"
)

// TestUninstallPathsReadDistinctHives guards a real defect. regAppPathUser and
// regAppPath64 are the same key path text under different hives, and
// nativeOpenKey used to hard-code HKLM. The per-user branch therefore reopened
// the machine's 64-bit uninstall list and returned all 82 of those programs a
// second time: GetInstalledApps reported 265 rows where the registry holds 188,
// and the 83 duplicated names were reported as separate applications.
//
// The assertion is a disequality rather than a row count, because a row count
// changes whenever the machine's software changes; the two branches reading the
// same hive cannot stay true as software is installed.
func TestUninstallPathsReadDistinctHives(t *testing.T) {
	machine64 := readUninstallEntries(hklm, regAppPath64)
	wow6432 := readUninstallEntries(hklm, regAppPath32)
	perUser := readUninstallEntries(hkcu, regAppPathUser)

	if len(machine64) == 0 {
		t.Fatal("no 64-bit uninstall entries; the probe cannot tell the hives apart")
	}
	if len(perUser) == 0 {
		t.Fatal("no per-user uninstall entries; this machine has none, so the defect cannot be reproduced")
	}
	if regAppPathUser == regAppPath64 {
		t.Logf("regAppPathUser and regAppPath64 are the same path text (%q), "+
			"so only the hive argument separates them", regAppPath64)
	}
	if equalStrings(machine64, perUser) {
		t.Errorf("HKCU branch returned the same %d rows as HKLM; it is reading the wrong hive", len(machine64))
	}

	// Assert through GetInstalledApps rather than only through the helper. An
	// earlier version of this test called the helper directly and therefore
	// proved the helper can distinguish the hives, not that the collector uses
	// the right one — so pointing the per-user entry back at HKLM left it green.
	all, err := GetInstalledApps(nil)
	if err != nil {
		t.Fatalf("GetInstalledApps: %v", err)
	}
	want := len(machine64) + len(wow6432) + len(perUser)
	if len(all) != want {
		t.Errorf("GetInstalledApps returned %d rows, want %d (HKLM64=%d + WOW6432Node=%d + HKCU=%d); "+
			"a per-user path read from HKLM repeats the machine's 64-bit list",
			len(all), want, len(machine64), len(wow6432), len(perUser))
	}
	t.Logf("HKLM64=%d WOW6432Node=%d HKCU=%d GetInstalledApps=%d",
		len(machine64), len(wow6432), len(perUser), len(all))
}

func equalStrings(a, b []model.InstalledApp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].DisplayName != b[i].DisplayName {
			return false
		}
	}
	return true
}

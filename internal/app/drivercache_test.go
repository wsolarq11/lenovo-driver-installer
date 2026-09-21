package app

import (
	"path/filepath"
	"testing"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/model"
)

func TestDriverListCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	drivers := []*model.Driver{{DriverCode: "d1", FileName: "d1.exe"}}
	if err := saveDriverListCache(path, "248", api.SourceDrivers{Source: "QuickFix", Drivers: drivers}); err != nil {
		t.Fatal(err)
	}
	got, ok := loadDriverListCache(path, "248")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Source != "QuickFix" || len(got.Drivers) != 1 || got.Drivers[0].DriverCode != "d1" {
		t.Fatalf("got = %#v", got)
	}
	if got.DriftWarning == "" {
		t.Fatal("stale cache must carry a warning")
	}
}

func TestDriverListCacheMiss(t *testing.T) {
	dir := t.TempDir()
	if _, ok := loadDriverListCache(filepath.Join(dir, "missing.json"), "248"); ok {
		t.Fatal("missing file must miss")
	}
}

func TestDriverListCacheOSIDMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	if err := saveDriverListCache(path, "42", api.SourceDrivers{Source: "Web", Drivers: []*model.Driver{{DriverCode: "d1"}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDriverListCache(path, "248"); ok {
		t.Fatal("OSID mismatch must miss")
	}
}

func TestDriverListCacheEmptyDrivers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	if err := saveDriverListCache(path, "248", api.SourceDrivers{Source: "Web"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadDriverListCache(path, "248"); ok {
		t.Fatal("empty driver list must miss")
	}
}

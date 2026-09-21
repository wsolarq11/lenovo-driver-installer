package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lenovo-driver/internal/api"
	"lenovo-driver/internal/model"
)

// driverListCache is the persisted last-good driver list used as an escape
// hatch when both Lenovo endpoints are dead. It is never a source of truth: it
// only keeps the tool able to show a stale list (clearly marked) instead of
// failing outright when the borrowed private interfaces are taken down or
// gated.
type driverListCache struct {
	FetchedAt time.Time       `json:"fetchedAt"`
	OSID      string          `json:"osid"`
	Source    string          `json:"source"`
	Drivers   []*model.Driver `json:"drivers"`
}

// driverListCachePath returns the per-OSID cache file under the app's cache
// directory (the stable audit artifact dir by default, a temp dir in tests).
func (a *App) driverListCachePath(osID string) string {
	return filepath.Join(a.driverListCacheDir, "driver_list_"+osID+".json")
}

// saveDriverListCache persists the last-good list. It is best-effort: a cache
// write failure must never fail the primary load path.
func saveDriverListCache(path, osID string, result api.SourceDrivers) error {
	cache := driverListCache{
		FetchedAt: time.Now().UTC(),
		OSID:      osID,
		Source:    result.Source,
		Drivers:   result.Drivers,
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// loadDriverListCache returns the persisted last-good list, if a valid one
// exists at path. The returned DriftWarning carries the staleness so callers
// surface "stale cache" distinctly from a live load.
func loadDriverListCache(path, osID string) (api.SourceDrivers, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return api.SourceDrivers{}, false
	}
	var cache driverListCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return api.SourceDrivers{}, false
	}
	if cache.OSID != osID || len(cache.Drivers) == 0 {
		return api.SourceDrivers{}, false
	}
	age := time.Since(cache.FetchedAt)
	warning := fmt.Sprintf("Lenovo endpoints unavailable; using cached driver list from %s (%s old)",
		cache.FetchedAt.Format("2006-01-02"), age.Round(time.Hour))
	return api.SourceDrivers{Source: cache.Source, Drivers: cache.Drivers, DriftWarning: warning}, true
}

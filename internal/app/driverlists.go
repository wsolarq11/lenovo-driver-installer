package app

// Official-list transport: fetching a driver list per OSID, memoizing it for the
// run, persisting the last-good copy, and degrading to that copy when both
// Lenovo endpoints are unreachable. The comparison pipeline consumes these; it
// does not decide how they are fetched.

import (
	"context"
	"fmt"
	"sync"

	"lenovo-driver/internal/api"
)

func (a *App) cachedDriverObjects(ctx context.Context, vc *ViewContext, osID, preferredSource string) (api.SourceDrivers, error) {
	key := osID + "|" + preferredSource
	a.osDriverCacheMu.Lock()
	if a.osDriverCache == nil {
		a.osDriverCache = map[string]api.SourceDrivers{}
	} else if result, ok := a.osDriverCache[key]; ok {
		a.osDriverCacheMu.Unlock()
		return result, nil
	}
	a.osDriverCacheMu.Unlock()

	result, err := a.APIClient.GetDriverObjects(ctx, vc.CategoryID, osID, preferredSource)
	if err != nil {
		// Escape hatch: both Lenovo endpoints are dead or gated, fall back to
		// the last-good persisted list (clearly marked stale) instead of
		// failing outright.
		if cached, ok := loadDriverListCache(a.driverListCachePath(osID), osID); ok {
			a.Log(ctx, "Driver list for OSID "+osID+": "+cached.DriftWarning, "WARN")
			a.osDriverCacheMu.Lock()
			a.osDriverCache[key] = cached
			a.osDriverCacheMu.Unlock()
			return cached, nil
		}
		return api.SourceDrivers{}, err
	}
	if saveErr := saveDriverListCache(a.driverListCachePath(osID), osID, result); saveErr != nil {
		a.Log(ctx, "Could not persist driver list cache for OSID "+osID+": "+saveErr.Error(), "WARN")
	}
	if result.DriftWarning != "" {
		a.Log(ctx, "Driver list for OSID "+osID+": "+result.DriftWarning+" (fell back to "+result.Source+")", "WARN")
	}
	a.osDriverCacheMu.Lock()
	a.osDriverCache[key] = result
	a.osDriverCacheMu.Unlock()
	return result, nil
}

// toleratedDriverListMiss reports why a driver list is unusable under the soft-
// miss policy: a fetch error, or an official list with no rows. A nil return
// means the list is usable. Both the cross-OS merge and the alternate-OS source
// map share this single fetch/tolerate policy.
func toleratedDriverListMiss(err error, result api.SourceDrivers) error {
	if err != nil {
		return err
	}
	if len(result.Drivers) == 0 {
		return fmt.Errorf("the official driver list was empty")
	}
	return nil
}

// driverListLoad is one OSID's fetch outcome, kept in input order so that
// callers can apply their per-OSID policy deterministically after the parallel
// fetch phase completes.
type driverListLoad struct {
	osID   string
	result api.SourceDrivers
	err    error
}

// loadDriverListsForOSIDs fetches the cached driver lists for a group of
// independent OSIDs in parallel (the reads are unrelated network calls that
// only benefit from concurrency), preserving the input order in the returned
// slice. The soft-miss policy is applied by the caller per slot in order.
func (a *App) loadDriverListsForOSIDs(ctx context.Context, vc *ViewContext, osIDs []string, preferredSource string) []driverListLoad {
	out := make([]driverListLoad, len(osIDs))
	for i, osID := range osIDs {
		out[i].osID = osID
	}
	var wg sync.WaitGroup
	for i, osID := range osIDs {
		wg.Add(1)
		go func(i int, osID string) {
			defer wg.Done()
			out[i].result, out[i].err = a.cachedDriverObjects(ctx, vc, osID, preferredSource)
		}(i, osID)
	}
	wg.Wait()
	return out
}

// loadDriverList returns the cached driver list for an OSID, logging a WARN and
// returning ok=false when the source is unavailable or empty. Both the
// cross-OS merge and the alternate-OS source map share this single
// fetch/tolerate policy.
func (a *App) loadDriverList(ctx context.Context, vc *ViewContext, osID, preferredSource string) (api.SourceDrivers, bool) {
	result, err := a.cachedDriverObjects(ctx, vc, osID, preferredSource)
	if miss := toleratedDriverListMiss(err, result); miss != nil {
		a.Log(ctx, "Could not load a driver list for OSID "+osID+": "+miss.Error(), "WARN")
		return api.SourceDrivers{}, false
	}
	return result, true
}

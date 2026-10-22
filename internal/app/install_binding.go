package app

import (
	"lenovo-driver/internal/compare"
	"lenovo-driver/internal/model"
)

// Post-install binding verdicts. These are pure functions kept apart from the
// IO-heavy verification pass in install.go so the rule that decides whether a
// run may call a driver installed has one owner and no side effects.

// installBindingVerdict classifies a post-install recheck. Bound means the
// active local version is now the package version; staged means the version
// moved but has not reached the package version; unchanged-same means the
// machine already ran exactly this package version, so the reinstall is a
// confirmed no-op; unchanged means the version did not move at all, which is
// also what an installer that silently did nothing produces; and undetected
// means no comparable version could be read. Only bound, staged and
// unchanged-same are evidence that the package took effect.
//
// The value itself decides comparability, never the driver family. Keying it on
// the family name threw away real evidence: a software-versioned driver that
// installed correctly reads its real version back from the installed-app list
// (Energy Management reports 15.11.29.65), and calling that "uncomparable"
// reported a successful install as unverified. The only unusable value is the
// provisioning placeholder, which equals itself no matter what happened; on 82JQ
// a drill for DRV202102040007 logged "Install success" with "Recheck: unchanged"
// while the machine gained nothing.
//
// The comparison itself is compare.Order, the same one CompareDriverStatus uses.
// Raw string equality was used here before and could never succeed for a
// composite package: the WLAN rows carry
// "Intel_22.10.0.7/Realtek8852AE_6001.0.10.336/Mediatek_3.0.1.1314" in Version,
// which no device's local version can equal, so those drivers could never be
// reported as bound no matter what happened on the machine.
func installBindingVerdict(before string, after model.LocalEvidence, packageVersion, vendor string) bindingVerdict {
	// Only a single measured version can confirm a binding. A conflicted,
	// provisioned, absent, or unobserved after-state carries no number to compare
	// against the package, so the recheck reports it as undetected rather than
	// picking one device's version and calling the install confirmed.
	afterRaw := after.MeasuredVersion()
	afterV := compare.ResolveComparableVersion(afterRaw, vendor)
	packageV := compare.ResolveComparableVersion(packageVersion, vendor)
	if !model.IsMeasuredLocalVersion(afterRaw) || afterV == nil {
		return bindingUndetected
	}
	switch compare.Order(afterV, packageV) {
	case compare.OrderUndecided:
		return bindingUndetected
	case compare.OrderEqual:
		if compare.Order(compare.ResolveComparableVersion(before, vendor), packageV) == compare.OrderEqual {
			return bindingUnchangedSame
		}
		return bindingBound
	default:
		if compare.Order(compare.ResolveComparableVersion(before, vendor), afterV) == compare.OrderEqual {
			return bindingUnchanged
		}
		return bindingStaged
	}
}

// bindingVerdict is what a post-install recheck can conclude. It was a bare
// string switched at the call site, where a `default:` branch quietly absorbed
// every unrecognized value — including the zero value, which had no meaning and
// therefore no honest log line. The zero value is now Undetected, the
// no-information verdict, so a forgotten assignment cannot read as success.
type bindingVerdict int

const (
	// bindingUndetected is the ZERO VALUE: no comparable version could be read,
	// so the recheck has nothing to say. It never confirms an install.
	bindingUndetected bindingVerdict = iota
	// bindingBound means the active local version is now the package version.
	bindingBound
	// bindingStaged means the version moved but has not reached the package
	// version yet (a reboot may be pending).
	bindingStaged
	// bindingUnchangedSame means the machine already ran exactly this package
	// version, so the reinstall is a confirmed no-op.
	bindingUnchangedSame
	// bindingUnchanged means the version did not move at all — what an installer
	// that silently did nothing also produces.
	bindingUnchanged
)

func (v bindingVerdict) String() string {
	switch v {
	case bindingBound:
		return "bound"
	case bindingStaged:
		return "staged"
	case bindingUnchangedSame:
		return "unchanged-same"
	case bindingUnchanged:
		return "unchanged"
	default:
		return "undetected"
	}
}

// confirmsEffect reports whether this verdict is evidence that the package took
// effect. It is the single gate between the recheck and the run summary, so a
// driver cannot be reported as installed on the strength of its exit code alone.
func (v bindingVerdict) confirmsEffect() bool {
	switch v {
	case bindingBound, bindingStaged, bindingUnchangedSame:
		return true
	default:
		return false
	}
}

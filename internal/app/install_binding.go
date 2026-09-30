package app

import "lenovo-driver/internal/model"

// Post-install binding verdicts. These are pure functions kept apart from the
// IO-heavy verification pass in install.go so the rule that decides whether a
// run may call a driver installed has one owner and no side effects.

// installBindingLabel classifies a post-install recheck. "bound" means the
// active local version is now the package version; "staged" means the version
// moved but has not reached the package version; "unchanged-same" means the
// machine already ran exactly this package version, so the reinstall is a
// confirmed no-op; "unchanged" means the version did not move at all, which is
// also what an installer that silently did nothing produces; and "undetected"
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
func installBindingLabel(before, after, packageVersion string) string {
	switch {
	case !model.IsMeasuredLocalVersion(after):
		return "undetected"
	case after == packageVersion && before == packageVersion:
		return "unchanged-same"
	case after == packageVersion:
		return "bound"
	case before == after:
		return "unchanged"
	default:
		return "staged"
	}
}

// installBindingConfirmsEffect reports whether a binding label is evidence that
// the package actually took effect. This is the single gate between the recheck
// and the run summary, so a driver cannot be reported as installed on the
// strength of its exit code alone.
func installBindingConfirmsEffect(binding string) bool {
	switch binding {
	case "bound", "staged", "unchanged-same":
		return true
	default:
		return false
	}
}

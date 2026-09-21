package trust

import "strings"

// RevocationFallback is invoked with the file path when revocation checking was
// unavailable (CRL/OCSP offline) and verification degraded to chain-only plus
// the Lenovo signer check. It is nil by default; the orchestration layer may
// set it to record the degradation.
var RevocationFallback func(path string)

// isLenovoSubject reports whether a certificate subject names Lenovo as its
// organization. The match is organization-level because Lenovo signs official
// packages under several legal entities (e.g. "CN=Lenovo, O=Lenovo..." and
// "Lenovo (Beijing) Limited"); a single exact subject would reject valid
// packages.
func isLenovoSubject(subject string) bool {
	return strings.Contains(strings.ToLower(subject), "lenovo")
}

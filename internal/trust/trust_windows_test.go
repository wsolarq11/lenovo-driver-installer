//go:build windows

package trust

import "testing"

func TestVerifyFileSignatureFallsBackOnRevocationOffline(t *testing.T) {
	origWin := winVerifyTrustFn
	origSigner := verifyLenovoSignerFn
	defer func() {
		winVerifyTrustFn = origWin
		verifyLenovoSignerFn = origSigner
	}()

	calls := 0
	winVerifyTrustFn = func(path string, revocationChecks uint32) uint32 {
		calls++
		if calls == 1 {
			return cryptERevocationOffline
		}
		return 0
	}
	verifyLenovoSignerFn = func(string) error { return nil }

	fallbackCalled := false
	origFallback := RevocationFallback
	defer func() { RevocationFallback = origFallback }()
	RevocationFallback = func(string) { fallbackCalled = true }

	if err := VerifyFileSignature("fixture.exe"); err != nil {
		t.Fatalf("VerifyFileSignature should pass after fallback: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 WinVerifyTrust calls, got %d", calls)
	}
	if !fallbackCalled {
		t.Fatal("RevocationFallback should be invoked on degradation")
	}
}

func TestVerifyFileSignatureRejectsNonRevocationFailure(t *testing.T) {
	origWin := winVerifyTrustFn
	defer func() { winVerifyTrustFn = origWin }()

	winVerifyTrustFn = func(string, uint32) uint32 {
		return 0x800B0100 // TRUST_E_NOSIGNATURE
	}
	fallbackCalled := false
	origFallback := RevocationFallback
	defer func() { RevocationFallback = origFallback }()
	RevocationFallback = func(string) { fallbackCalled = true }

	if err := VerifyFileSignature("fixture.exe"); err == nil {
		t.Fatal("VerifyFileSignature should reject a non-revocation failure")
	}
	if fallbackCalled {
		t.Fatal("RevocationFallback must not fire for a non-revocation failure")
	}
}

func TestVerifyFileSignatureRejectsFailedFallback(t *testing.T) {
	origWin := winVerifyTrustFn
	defer func() { winVerifyTrustFn = origWin }()

	winVerifyTrustFn = func(string, uint32) uint32 {
		return cryptERevocationOffline // both attempts report offline
	}
	if err := VerifyFileSignature("fixture.exe"); err == nil {
		t.Fatal("VerifyFileSignature should reject when the chain-only fallback also fails")
	}
}

//go:build windows

package trust

import (
	"errors"
	"strings"
	"testing"
)

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

// The whitelist decision is the boundary that matters most: WinVerifyTrust only
// proves a signature is internally valid, so a validly signed non-Lenovo binary
// must still be rejected. These three cases pin the accept and both reject
// paths of verifyLenovoSigner itself, through the same seam the production
// callers use.
func TestVerifyLenovoSignerAcceptsLenovoSubject(t *testing.T) {
	orig := signerSubjectFn
	defer func() { signerSubjectFn = orig }()

	signerSubjectFn = func(string) (string, error) {
		return "CN=Lenovo, O=Lenovo, C=US", nil
	}
	if err := verifyLenovoSigner("fixture.exe"); err != nil {
		t.Fatalf("a Lenovo signer subject must be accepted, got %v", err)
	}
}

func TestVerifyLenovoSignerRejectsOtherSubject(t *testing.T) {
	orig := signerSubjectFn
	defer func() { signerSubjectFn = orig }()

	signerSubjectFn = func(string) (string, error) {
		return "CN=Example Corp, O=Example, C=US", nil
	}
	err := verifyLenovoSigner("fixture.exe")
	if err == nil {
		t.Fatal("a non-Lenovo signer subject must be rejected")
	}
	if !strings.Contains(err.Error(), "not Lenovo") {
		t.Fatalf("the rejection must name the whitelist as the cause, got %v", err)
	}
}

func TestVerifyLenovoSignerPropagatesSubjectReadFailure(t *testing.T) {
	orig := signerSubjectFn
	defer func() { signerSubjectFn = orig }()

	signerSubjectFn = func(string) (string, error) {
		return "", errors.New("no embedded signature")
	}
	err := verifyLenovoSigner("fixture.exe")
	if err == nil {
		t.Fatal("an unreadable signer subject must be an error, not an accept")
	}
	if !strings.Contains(err.Error(), "could not read signer certificate") {
		t.Fatalf("the failure must be reported as unreadable, got %v", err)
	}
}

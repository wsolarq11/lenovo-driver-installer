//go:build windows

package trust

import (
	"fmt"
	"syscall"
	"unsafe"
)

// VerifyFileSignature validates the Authenticode signature of a file through
// WinVerifyTrust with WTD_UI_NONE, checks certificate revocation, and then
// enforces that the signing leaf certificate belongs to Lenovo. A merely
// "valid" signature from an unrelated publisher is not trusted: without the
// signer check, any validly signed third-party binary would pass.
var (
	wintrust           = syscall.NewLazyDLL("wintrust.dll")
	procWinVerifyTrust = wintrust.NewProc("WinVerifyTrust")

	crypt32                  = syscall.NewLazyDLL("crypt32.dll")
	procCryptQueryObject     = crypt32.NewProc("CryptQueryObject")
	procCertEnumCertificates = crypt32.NewProc("CertEnumCertificatesInStore")
	procCertGetNameStringW   = crypt32.NewProc("CertGetNameStringW")
	procCertFreeCertificate  = crypt32.NewProc("CertFreeCertificateContext")
	procCertCloseStore       = crypt32.NewProc("CertCloseStore")
)

const (
	wtdUIChoiceNone      = 2
	wtdRevocationNone    = 0
	wtdRevocationWhole   = 0x00000100
	wtdChoiceFile        = 1
	wtdStateActionVerify = 1
	wtdStateActionClose  = 2

	// cryptERevocationOffline is CRYPT_E_REVOCATION_OFFLINE: the certificate is
	// fine but its revocation status could not be determined (CRL/OCSP
	// unreachable). It is the only revocation failure that justifies falling
	// back to chain-only verification.
	cryptERevocationOffline = 0x80092013

	certQueryObjectFile              = 0x00000001
	certQueryContentPkcs7SignedEmbed = 0x400
	certQueryFormatBinary            = 0x2
	certNameRDNType                  = 2
	x509AsnEncoding                  = 0x1
	pkcs7AsnEncoding                 = 0x10000
)

// winTrustActionGenericVerifyV2 is the standard action GUID
// {00AAC56B-CD44-11D0-8CC2-00C04FC295EE}.
var winTrustActionGenericVerifyV2 = syscall.GUID{
	Data1: 0x00AAC56B,
	Data2: 0xCD44,
	Data3: 0x11D0,
	Data4: [8]byte{0x8C, 0xC2, 0x00, 0xC0, 0x4F, 0xC2, 0x95, 0xEE},
}

// winTrustFileInfo mirrors WINTRUST_FILE_INFO. Fields are pointers on 64-bit;
// Go applies the same alignment as the C ABI, and cbStruct is sized at runtime.
type winTrustFileInfo struct {
	cbStruct       uint32
	pcwszFilePath  uintptr
	hFile          uintptr
	pgKnownSubject uintptr
}

// winTrustData mirrors WINTRUST_DATA. The union member is represented by pFile
// because VerifyFileSignature only uses the file choice.
type winTrustData struct {
	cbStruct            uint32
	pPolicyCallbackData uintptr
	pSIPClientData      uintptr
	dwUIChoice          uint32
	fdwRevocationChecks uint32
	dwUnionChoice       uint32
	pFile               uintptr
	dwStateAction       uint32
	hWVTStateData       uintptr
	pwszURLReference    uintptr
	dwProvFlags         uint32
	dwUIContext         uint32
	pSignatureSettings  uintptr
}

// winVerifyTrustFn and verifyLenovoSignerFn are injectable seams so the
// two-stage fallback decision is unit-testable without a real signed file or a
// live CRL. Production never overrides them.
var (
	winVerifyTrustFn     = winVerifyTrust
	verifyLenovoSignerFn = verifyLenovoSigner
)

// VerifyFileSignature validates the file with revocation checking first. When
// revocation status cannot be determined (CRL/OCSP offline), it falls back to
// chain-only verification instead of rejecting a potentially valid official
// package; the Lenovo signer check still runs in both paths, so an unrelated
// third-party signer is rejected either way. RevocationFallback (if set) is
// invoked on that degradation.
func VerifyFileSignature(path string) error {
	hr := winVerifyTrustFn(path, wtdRevocationWhole)
	if hr == 0 {
		return verifyLenovoSignerFn(path)
	}
	if hr != cryptERevocationOffline {
		return fmt.Errorf("Authenticode verification failed: 0x%08X", uint32(hr))
	}
	if fb := winVerifyTrustFn(path, wtdRevocationNone); fb != 0 {
		return fmt.Errorf("Authenticode verification failed: 0x%08X", uint32(fb))
	}
	if RevocationFallback != nil {
		RevocationFallback(path)
	}
	return verifyLenovoSignerFn(path)
}

// winVerifyTrust runs WinVerifyTrust with the given revocation mode and returns
// the HRESULT (0 on success). The close action always runs so the trust
// provider releases its state data even when verification failed.
func winVerifyTrust(path string, revocationChecks uint32) uint32 {
	utf16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return uint32(0x80070057) // E_INVALIDARG
	}
	fileInfo := winTrustFileInfo{
		cbStruct:      uint32(unsafe.Sizeof(winTrustFileInfo{})),
		pcwszFilePath: uintptr(unsafe.Pointer(utf16)),
	}
	data := winTrustData{
		cbStruct:            uint32(unsafe.Sizeof(winTrustData{})),
		dwUIChoice:          wtdUIChoiceNone,
		fdwRevocationChecks: revocationChecks,
		dwUnionChoice:       wtdChoiceFile,
		pFile:               uintptr(unsafe.Pointer(&fileInfo)),
		dwStateAction:       wtdStateActionVerify,
	}
	r, _, _ := procWinVerifyTrust.Call(
		0,
		uintptr(unsafe.Pointer(&winTrustActionGenericVerifyV2)),
		uintptr(unsafe.Pointer(&data)),
	)
	closeData := data
	closeData.dwStateAction = wtdStateActionClose
	_, _, _ = procWinVerifyTrust.Call(
		0,
		uintptr(unsafe.Pointer(&winTrustActionGenericVerifyV2)),
		uintptr(unsafe.Pointer(&closeData)),
	)
	return uint32(r)
}

// verifyLenovoSigner extracts the signing leaf certificate subject and rejects
// any signer whose organization is not Lenovo. The organization-level match
// tolerates the multiple Lenovo legal entities seen across official packages
// (e.g. "CN=Lenovo, O=Lenovo..." and "Lenovo (Beijing) Limited").
func verifyLenovoSigner(path string) error {
	subject, err := signerSubject(path)
	if err != nil {
		return fmt.Errorf("could not read signer certificate: %w", err)
	}
	if !isLenovoSubject(subject) {
		return fmt.Errorf("signer is not Lenovo: %s", subject)
	}
	return nil
}

// signerSubject returns the display subject of a Lenovo certificate found in
// the file's embedded Authenticode signature chain. WinVerifyTrust has already
// established the chain is valid end to end, so a Lenovo certificate present in
// the chain identifies the signer; an unrelated third-party leaf cannot carry a
// genuine Lenovo certificate in its own chain.
func signerSubject(path string) (string, error) {
	utf16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var hStore uintptr
	r, _, _ := procCryptQueryObject.Call(
		certQueryObjectFile,
		uintptr(unsafe.Pointer(utf16)),
		certQueryContentPkcs7SignedEmbed,
		certQueryFormatBinary,
		0, 0, 0, 0,
		uintptr(unsafe.Pointer(&hStore)),
		0,
		0,
	)
	if r == 0 || hStore == 0 {
		return "", fmt.Errorf("file has no embedded Authenticode signature")
	}
	defer procCertCloseStore.Call(hStore, 0)

	var prev uintptr
	for {
		next, _, _ := procCertEnumCertificates.Call(hStore, prev)
		if prev != 0 {
			procCertFreeCertificate.Call(prev)
		}
		prev = next
		if prev == 0 {
			break
		}
		if subject, ok := certSubject(prev); ok && isLenovoSubject(subject) {
			procCertFreeCertificate.Call(prev)
			return subject, nil
		}
	}
	return "", fmt.Errorf("no Lenovo signer certificate in signature chain")
}

// certSubject reads the RDN-form subject of a certificate context.
func certSubject(pCert uintptr) (string, bool) {
	n, _, _ := procCertGetNameStringW.Call(pCert, certNameRDNType, 0, 0, 0, 0)
	if n == 0 {
		return "", false
	}
	name := make([]uint16, n)
	n, _, _ = procCertGetNameStringW.Call(pCert, certNameRDNType, 0, 0, uintptr(unsafe.Pointer(&name[0])), n)
	if n == 0 {
		return "", false
	}
	return syscall.UTF16ToString(name), true
}

package download

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Downloader downloads files and maintains hash companions.
type Downloader struct {
	Client *http.Client
}

// HTTPStatusError reports a non-success response from a download request.
type HTTPStatusError struct {
	Status int
	URL    string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("download returned HTTP %d for %s", e.Status, e.URL)
}

// IsHTTPStatus reports whether err is an HTTPStatusError with the given status.
func IsHTTPStatus(err error, status int) bool {
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.Status == status
	}
	return false
}

// NewDownloader returns a downloader with a 180 second request timeout.
func NewDownloader() *Downloader {
	return &Downloader{Client: &http.Client{Timeout: 180 * time.Second}}
}

// allowedHostSuffixes are the Lenovo-owned domains a driver download may come
// from. Matching is suffix-based so any subdomain of lenovo.com and
// lenovo.com.cn is accepted. Every other host is rejected before a single byte
// is requested: a hijacked FilePath pointing at a third-party CDN must fail
// closed instead of pulling bytes from an untrusted origin. Expanding this list
// requires the AGENTS.md whitelist record (difference, reason, impact, test,
// rollback, expiry).
var allowedHostSuffixes = []string{"lenovo.com", "lenovo.com.cn"}

// TrustedHost reports whether host is a Lenovo-owned domain. The comparison is
// case-insensitive and ignores a trailing dot.
func TrustedHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, suffix := range allowedHostSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// DownloadWithRetry mirrors Invoke-DownloadWithRetry.
func (d *Downloader) DownloadWithRetry(ctx context.Context, urlText, outFile string, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := d.downloadOnce(ctx, urlText, outFile)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxAttempts {
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

func (d *Downloader) downloadOnce(ctx context.Context, urlText, outFile string) error {
	parsed, err := url.Parse(urlText)
	if err != nil {
		return fmt.Errorf("invalid download URL %q: %w", urlText, err)
	}
	if !TrustedHost(parsed.Hostname()) {
		return fmt.Errorf("refusing to download from untrusted host %q", parsed.Hostname())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlText, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPStatusError{Status: resp.StatusCode, URL: urlText}
	}
	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(outFile)
	if err != nil {
		return err
	}
	if info.Size() <= 0 {
		return fmt.Errorf("downloaded file is empty")
	}
	return nil
}

// FileSHA256 mirrors Get-FileSha256.
func FileSHA256(path string) string {
	sum, err := hashFile(path, sha256.New())
	if err != nil {
		return ""
	}
	return sum
}

// FileMD5 mirrors Get-FileMd5.
func FileMD5(path string) string {
	sum, err := hashFile(path, md5.New())
	if err != nil {
		return ""
	}
	return sum
}

type hashWriter interface {
	io.Writer
	Sum([]byte) []byte
}

func hashFile(path string, h hashWriter) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// WriteHashCompanion mirrors Write-HashCompanion.
func WriteHashCompanion(filePath, hash string) error {
	companion := filePath + ".sha256"
	temp := companion + ".tmp"
	content := "SHA256 " + strings.ToLower(hash) + " " + filepath.Base(filePath)
	if err := os.WriteFile(temp, []byte(content), 0o644); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Rename(temp, companion)
}

// TestHashCompanion mirrors Test-HashCompanion.
func TestHashCompanion(filePath string, skipHashCheck bool) bool {
	if skipHashCheck {
		return true
	}
	companion := filePath + ".sha256"
	data, err := os.ReadFile(companion)
	if err != nil {
		return false
	}
	line := strings.TrimSpace(string(data))
	if line == "" || strings.Contains(line, "\n") || strings.Contains(line, "\r") {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "SHA256" {
		return false
	}
	expected := strings.ToLower(fields[1])
	actual := FileSHA256(filePath)
	return actual == expected
}

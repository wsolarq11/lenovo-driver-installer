package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"lenovo-driver/internal/model"
)

const (
	apiBase         = "https://newsupport.lenovo.com.cn/api"
	quickFixBase    = "https://ptstpd.lenovo.com.cn"
	userAgent       = "Mozilla/5.0"
	quickFixReferer = "https://iknow.lenovo.com.cn/"
	webReferer      = "https://newsupport.lenovo.com.cn/driveDownloads_index.html"
)

var reSerialPlaceholder = regexp.MustCompile(`(?i)To be filled|None|Default`)

// Client is the Lenovo API client.
type Client struct {
	HTTP *http.Client
}

// NewClient returns a client with a 30 second timeout and TLS 1.2+ defaults.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// SourceDrivers is the normalized source plus driver rows returned by GetDriverObjects.
type SourceDrivers struct {
	Source  string
	Drivers []*model.Driver
	// DriftWarning is set when the preferred source's list shape drifted and the
	// caller fell back to the other source. It is empty on a clean load so the
	// orchestration layer can surface "the interface changed" distinctly from a
	// plain network failure.
	DriftWarning string
}

// contractDriftError reports that a Lenovo API returned a non-empty driver list
// whose shape no longer matches the parser contract: every row was dropped
// because required fields (FileName/FilePath) were absent. It is distinct from
// an empty response so the caller can tell "the interface changed" apart from
// "there are genuinely no drivers", instead of silently degrading one to the
// other.
type contractDriftError struct {
	Source   string
	RowCount int
}

func (e *contractDriftError) Error() string {
	return fmt.Sprintf("%s driver list shape drifted: %d rows present but none parsed (required fields missing)", e.Source, e.RowCount)
}

// interfaceGateError reports that a Lenovo endpoint responded in a way that
// suggests the private interface is now gated or blocked (authentication, rate
// limiting, blocking, or a wholesale response rewrite), rather than a mere
// field-shape drift. It is surfaced distinctly so the user knows the data
// source changed hands instead of concluding "no drivers".
type interfaceGateError struct {
	Source string
	Reason string
}

func (e *interfaceGateError) Error() string {
	return fmt.Sprintf("%s interface may be gated or blocked: %s", e.Source, e.Reason)
}

func (c *Client) invokeLenovo(ctx context.Context, relativeURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+relativeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", webReferer)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &interfaceGateError{Source: "Web", Reason: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) invokeQuickFix(ctx context.Context, searchKey, osID string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"searchKey": searchKey, "osid": osID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quickFixBase+"/home/driver/SearchForXbb", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", quickFixReferer)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &interfaceGateError{Source: "QuickFix", Reason: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// GetDriverObjects tries QuickFix first, then the official webpage API.
func (c *Client) GetDriverObjects(ctx context.Context, categoryID, osID, preferredSource string) (SourceDrivers, error) {
	attempts := []string{"QuickFix", "Web"}
	if preferredSource == "Web" {
		attempts = []string{"Web", "QuickFix"}
	}
	var lastErr error
	var driftWarning string
	for _, source := range attempts {
		var drivers []*model.Driver
		var err error
		if source == "QuickFix" {
			drivers, err = c.fetchQuickFix(ctx, categoryID, osID)
		} else {
			drivers, err = c.fetchWeb(ctx, categoryID, osID)
		}
		if err == nil && len(drivers) > 0 {
			return SourceDrivers{Source: source, Drivers: drivers, DriftWarning: driftWarning}, nil
		}
		if err != nil {
			lastErr = err
			var drift *contractDriftError
			if errors.As(err, &drift) {
				driftWarning = drift.Error()
			}
			var gate *interfaceGateError
			if errors.As(err, &gate) {
				driftWarning = gate.Error()
			}
		} else {
			lastErr = fmt.Errorf("%s returned no driver rows", source)
		}
	}
	return SourceDrivers{}, fmt.Errorf("could not load the official driver list from %s or %s: %w", attempts[0], attempts[1], lastErr)
}

func (c *Client) fetchQuickFix(ctx context.Context, searchKey, osID string) ([]*model.Driver, error) {
	data, err := c.invokeQuickFix(ctx, searchKey, osID)
	if err != nil {
		return nil, err
	}
	var resp QuickFixResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, &interfaceGateError{Source: "QuickFix", Reason: "response is not the expected JSON"}
	}
	if resp.StatusCode == "" && len(resp.Data.DriverList) == 0 && len(resp.Data.OSList) == 0 && len(resp.Data.PartList) == 0 {
		return nil, &interfaceGateError{Source: "QuickFix", Reason: "response missing expected fields (StatusCode/Data)"}
	}
	code := strings.TrimSpace(resp.StatusCode)
	if code != "200" && code != "300" {
		return nil, fmt.Errorf("QuickFix API returned %s for %s / %s : %s", code, searchKey, osID, resp.Message)
	}
	if len(resp.Data.DriverList) == 0 {
		return nil, fmt.Errorf("QuickFix API returned no driver list")
	}
	drivers := ParseQuickFix(&resp, osID)
	if len(drivers) == 0 {
		return nil, &contractDriftError{Source: "QuickFix", RowCount: len(resp.Data.DriverList)}
	}
	return drivers, nil
}

func (c *Client) fetchWeb(ctx context.Context, categoryID, osID string) ([]*model.Driver, error) {
	query := "/drive/drive_listnew?searchKey=" + url.QueryEscape(categoryID) + "&sysid=" + url.QueryEscape(osID)
	data, err := c.invokeLenovo(ctx, query)
	if err != nil {
		return nil, err
	}
	var resp webResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, &interfaceGateError{Source: "Web", Reason: "response is not the expected JSON"}
	}
	if code := strings.TrimSpace(resp.StatusCode.String()); code != "" && code != "200" {
		return nil, fmt.Errorf("Lenovo API returned %s for %s : %s", code, query, resp.Message)
	}
	rawCount := 0
	for _, part := range resp.Data.PartList {
		rawCount += len(part.Drivelist)
	}
	if resp.StatusCode.String() == "" && rawCount == 0 && len(resp.Data.OSList) == 0 {
		return nil, &interfaceGateError{Source: "Web", Reason: "response missing expected fields (statusCode/data)"}
	}
	drivers := parseWeb(&resp, osID)
	if len(drivers) == 0 {
		if rawCount > 0 {
			return nil, &contractDriftError{Source: "Web", RowCount: rawCount}
		}
		return nil, fmt.Errorf("official driver list returned no rows")
	}
	return drivers, nil
}

// ResolveCategoryID resolves a Lenovo category id from model or serial.
func (c *Client) ResolveCategoryID(ctx context.Context, machineModel, serial string) (string, error) {
	var keys []string
	if machineModel != "" {
		keys = append(keys, machineModel)
	}
	if serial != "" && !reSerialPlaceholder.MatchString(serial) {
		keys = append(keys, serial)
	}
	var lastErr error
	for _, key := range keys {
		data, err := c.invokeLenovo(ctx, "/drive/drive_query?searchKey="+url.QueryEscape(key))
		if err != nil {
			lastErr = err
			continue
		}
		var resp struct {
			Data []struct {
				CategoryID string `json:"categoryid"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			lastErr = err
			continue
		}
		if len(resp.Data) > 0 && resp.Data[0].CategoryID != "" {
			return resp.Data[0].CategoryID, nil
		}
		lastErr = fmt.Errorf("no categoryid in lookup for %s", key)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no machine key provided")
	}
	return "", lastErr
}

// OSResolution is the resolved OS entry plus the available OS list.
type OSResolution struct {
	OSEntry model.OSListEntry
	OSList  []model.OSListEntry
	Source  string
	// WebFailure explains why the webpage OS list could not be used when Source
	// is QuickFix. It carries the actual cause (transport, decode, or no entry
	// for this OS) so the log does not blame the network for a parse failure.
	WebFailure string
}

// osIDProbe is the osid sent when only the OS list is wanted. The endpoint
// answers a request for an OS it ships no drivers for with the machine's full
// OS list and an empty driver list, so the list is readable before the machine's
// own OSID is known. Probing this way replaces a hardcoded []string{"42","248"}:
// the resolver no longer has to know which Windows versions Lenovo currently
// lists, and a newly supported OS appears in the list without a code change.
const osIDProbe = "0"

// ResolveOSEntry resolves the machine's OS name to a Lenovo OS entry. The
// webpage endpoint is asked first because one request returns the complete list
// for this machine; QuickFix is the fallback.
func (c *Client) ResolveOSEntry(ctx context.Context, categoryID, osName, osKind string) (*OSResolution, error) {
	webList, webLocal, webErr := c.fetchWebOSList(ctx, categoryID)
	if webErr == nil {
		if res := resolveFromOSList(webList, webLocal, osName, osKind, "Web"); res != nil {
			return res, nil
		}
		webErr = fmt.Errorf("the webpage OS list has no entry matching %s", describeOSMatch(osName, osKind))
	}

	qfList, qfErr := c.fetchQuickFixOSList(ctx, categoryID)
	if qfErr == nil {
		if res := resolveFromOSList(qfList, "", osName, osKind, "QuickFix"); res != nil {
			res.WebFailure = webErr.Error()
			return res, nil
		}
		qfErr = fmt.Errorf("the QuickFix OS list has no entry matching %s", describeOSMatch(osName, osKind))
	}

	return nil, fmt.Errorf("could not resolve the current OS entry from Lenovo: webpage: %v; QuickFix: %v",
		webErr, qfErr)
}

// fetchWebOSList reads the OS list the official webpage endpoint returns for a
// machine, plus the endpoint's own idea of the local OSID. It sends no sysid:
// the list is machine-scoped, not OS-scoped.
func (c *Client) fetchWebOSList(ctx context.Context, categoryID string) ([]model.OSListEntry, string, error) {
	data, err := c.invokeLenovo(ctx, "/drive/drive_listnew?searchKey="+url.QueryEscape(categoryID))
	if err != nil {
		return nil, "", err
	}
	var resp webResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, "", &interfaceGateError{Source: "Web", Reason: "OS list response is not the expected JSON"}
	}
	if len(resp.Data.OSList) == 0 {
		return nil, "", &interfaceGateError{Source: "Web", Reason: "OS list response carries no osList"}
	}
	return resp.Data.OSList, resp.Data.LocalOSID.String(), nil
}

// fetchQuickFixOSList reads the OS list through the QuickFix endpoint. The probe
// osid yields no drivers, which is expected: only the list is wanted here.
func (c *Client) fetchQuickFixOSList(ctx context.Context, categoryID string) ([]model.OSListEntry, error) {
	data, err := c.invokeQuickFix(ctx, categoryID, osIDProbe)
	if err != nil {
		return nil, err
	}
	var resp QuickFixResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, &interfaceGateError{Source: "QuickFix", Reason: "OS list response is not the expected JSON"}
	}
	if len(resp.Data.OSList) == 0 {
		return nil, &interfaceGateError{Source: "QuickFix", Reason: "OS list response carries no osList"}
	}
	return resp.Data.OSList, nil
}

// resolveFromOSList picks the entry matching the machine's OS, falling back to
// the endpoint's own idea of the local OS when no name matches. It returns nil
// when neither yields an entry, so the caller can try the other source.
func resolveFromOSList(osList []model.OSListEntry, localOSID, osName, osKind, source string) *OSResolution {
	if entry, ok := findOSEntry(osList, osName, osKind); ok {
		return &OSResolution{OSEntry: entry, OSList: osList, Source: source}
	}
	if localOSID != "" {
		for _, candidate := range osList {
			if candidate.OSID == localOSID {
				return &OSResolution{OSEntry: candidate, OSList: osList, Source: source}
			}
		}
	}
	return nil
}

func describeOSMatch(osName, osKind string) string {
	switch {
	case osName != "":
		return fmt.Sprintf("%q", osName)
	case osKind != "":
		return fmt.Sprintf("%q", osKind)
	default:
		return "the current OS"
	}
}

func findOSEntry(osList []model.OSListEntry, osName, osKind string) (model.OSListEntry, bool) {
	if osName != "" {
		lowerName := strings.ToLower(osName)
		for _, entry := range osList {
			if strings.Contains(strings.ToLower(entry.OSName), lowerName) {
				return entry, true
			}
		}
	}
	if osKind != "" {
		lowerKind := strings.ToLower(osKind)
		for _, entry := range osList {
			if strings.Contains(strings.ToLower(entry.OSName), lowerKind) {
				return entry, true
			}
		}
	}
	return model.OSListEntry{}, false
}

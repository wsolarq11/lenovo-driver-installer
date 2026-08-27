package api

import (
	"bytes"
	"context"
	"encoding/json"
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
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// GetDriverObjects tries QuickFix first, then the official webpage API.
func (c *Client) GetDriverObjects(ctx context.Context, categoryID, osID, preferredSource string) SourceDrivers {
	attempts := []string{"QuickFix", "Web"}
	if preferredSource == "Web" {
		attempts = []string{"Web", "QuickFix"}
	}
	for _, source := range attempts {
		var drivers []*model.Driver
		var err error
		if source == "QuickFix" {
			drivers, err = c.fetchQuickFix(ctx, categoryID, osID)
		} else {
			drivers, err = c.fetchWeb(ctx, categoryID, osID)
		}
		if err == nil && len(drivers) > 0 {
			return SourceDrivers{Source: source, Drivers: drivers}
		}
	}
	return SourceDrivers{}
}

func (c *Client) fetchQuickFix(ctx context.Context, searchKey, osID string) ([]*model.Driver, error) {
	data, err := c.invokeQuickFix(ctx, searchKey, osID)
	if err != nil {
		return nil, err
	}
	var resp QuickFixResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(resp.StatusCode)
	if code != "200" && code != "300" {
		return nil, fmt.Errorf("QuickFix API returned %s for %s / %s : %s", code, searchKey, osID, resp.Message)
	}
	if len(resp.Data.DriverList) == 0 {
		return nil, fmt.Errorf("QuickFix API returned no driver list")
	}
	return ParseQuickFix(&resp, osID), nil
}

func (c *Client) fetchWeb(ctx context.Context, categoryID, osID string) ([]*model.Driver, error) {
	query := "/drive/drive_listnew?searchKey=" + url.QueryEscape(categoryID) + "&sysid=" + url.QueryEscape(osID)
	data, err := c.invokeLenovo(ctx, query)
	if err != nil {
		return nil, err
	}
	var resp WebResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if code := strings.TrimSpace(resp.StatusCode); code != "" && code != "200" {
		return nil, fmt.Errorf("Lenovo API returned %s for %s : %s", code, query, resp.Message)
	}
	drivers := ParseWeb(&resp, osID)
	if len(drivers) == 0 {
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
	if serial != "" && !regexp.MustCompile(`(?i)To be filled|None|Default`).MatchString(serial) {
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
}

// ResolveOSEntry mirrors Resolve-LenovoOsEntry.
func (c *Client) ResolveOSEntry(ctx context.Context, categoryID, osName, osKind string) (*OSResolution, error) {
	webData, err := c.invokeLenovo(ctx, "/drive/drive_listnew?searchKey="+url.QueryEscape(categoryID))
	if err == nil {
		var resp WebResponse
		if jsonErr := json.Unmarshal(webData, &resp); jsonErr == nil && len(resp.Data.OSList) > 0 {
			if entry, ok := findOSEntry(resp.Data.OSList, osName, osKind); ok {
				return &OSResolution{OSEntry: entry, OSList: resp.Data.OSList, Source: "Web"}, nil
			}
			for _, candidate := range resp.Data.OSList {
				if candidate.OSID == resp.Data.LocalOSID {
					return &OSResolution{OSEntry: candidate, OSList: resp.Data.OSList, Source: "Web"}, nil
				}
			}
		}
	}

	for _, candidateID := range []string{"42", "248"} {
		data, qfErr := c.invokeQuickFix(ctx, categoryID, candidateID)
		if qfErr != nil {
			continue
		}
		var resp QuickFixResponse
		if jsonErr := json.Unmarshal(data, &resp); jsonErr != nil {
			continue
		}
		if len(resp.Data.OSList) == 0 {
			continue
		}
		if entry, ok := findOSEntry(resp.Data.OSList, osName, osKind); ok {
			return &OSResolution{OSEntry: entry, OSList: resp.Data.OSList, Source: "QuickFix"}, nil
		}
	}
	return nil, fmt.Errorf("could not resolve current OS entry from Lenovo")
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

// GetRefreshedDriverURL refreshes a download URL from the current or alternate OS lists.
func (c *Client) GetRefreshedDriverURL(ctx context.Context, driver *model.Driver, categoryID, sysID string, osList []model.OSListEntry, latestAcrossOS, useQuickFix bool) (string, error) {
	type queryItem struct {
		osID string
	}
	queries := []queryItem{{osID: sysID}}
	if latestAcrossOS {
		for _, entry := range osList {
			if entry.OSID == sysID {
				continue
			}
			queries = append(queries, queryItem{osID: entry.OSID})
		}
	}
	var lastErr error
	for _, item := range queries {
		var drivers []*model.Driver
		var err error
		if useQuickFix {
			drivers, err = c.fetchQuickFix(ctx, categoryID, item.osID)
		} else {
			drivers, err = c.fetchWeb(ctx, categoryID, item.osID)
		}
		if err != nil {
			lastErr = err
			continue
		}
		for _, candidate := range drivers {
			if candidate.DriverCode == driver.DriverCode && candidate.FilePath != "" {
				return candidate.FilePath, nil
			}
		}
		lastErr = fmt.Errorf("driver %s has no refreshed URL for OSID %s", driver.DriverCode, item.osID)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("driver %s has no refreshed URL", driver.DriverCode)
	}
	return "", lastErr
}

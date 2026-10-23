package gui

// Deep-link URL parsing for lenovo-driver://audit links. Only the query is
// read; anything else in the URL is ignored, so a malformed link degrades to
// a plain refresh.

import (
	"net/url"
	"strings"
)

// urlScheme is the protocol the app registers and handles.
const urlScheme = "lenovo-driver"

// auditURL returns the first argument that is a URL of this app's scheme, ""
// when the command line carries none. Windows hands a deep link to the
// registered handler as an argument, so this is how a launch started from a
// link differs from a plain CLI run.
func auditURL(args []string) string {
	for _, arg := range args {
		if u, err := url.Parse(arg); err == nil && strings.EqualFold(u.Scheme, urlScheme) {
			return arg
		}
	}
	return ""
}

// deeplinkOSID extracts the osid query parameter of an audit deep link; ""
// selects the current system. A non-audit path or an unparseable URL yields
// "" as well: the link still refreshes, just without an OS override.
func deeplinkOSID(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if u.Scheme != urlScheme || (u.Host != "" && u.Host != "audit") && u.Path != "/audit" {
		return ""
	}
	return u.Query().Get("osid")
}

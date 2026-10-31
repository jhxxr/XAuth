package xmail

import (
	"net/http"
	"strings"
)

// AdminRoutes are the xmail endpoints listed in xmail's API.md that the
// XAuth admin proxy forwards. "*" stands for one path segment (an address,
// email ID, template ID). Legacy /admin/* routes and MCP are not included.
var AdminRoutes = []string{
	"GET mailboxes",
	"POST mailboxes",
	"POST mailboxes/batch",
	"DELETE mailboxes/batch",
	"POST mailboxes/batch/services",
	"GET mailboxes/*",
	"PATCH mailboxes/*",
	"DELETE mailboxes/*",
	"POST mailboxes/*/restore",
	"GET mailboxes/*/password",
	"POST mailboxes/*/password",
	"GET mailboxes/*/emails",
	"GET mailboxes/*/stats",
	"GET mailboxes/*/verification-code",
	"GET mailboxes/*/services",
	"POST mailboxes/*/services",
	"PATCH mailboxes/*/services",
	"DELETE mailboxes/*/services",

	"GET emails",
	"GET emails/starred",
	"POST emails/cleanup",
	"GET emails/*",
	"PATCH emails/*",
	"DELETE emails/*",
	"POST emails/*/read",
	"POST emails/*/star",

	"GET services",
	"POST services",
	"GET services/*",
	"PATCH services/*",
	"DELETE services/*",

	"GET trash",
	"DELETE trash",
	"POST trash/*/restore",
	"DELETE trash/*",

	"GET system",
	"GET system/stats",
	"GET system/domains",
	"POST system/domains",
	"PUT system/domains",
	"DELETE system/domains",
	"GET system/settings",
	"PATCH system/settings",
	"GET system/logs",
	"DELETE system/logs",
}

// ValidSegment reports whether s (already percent-decoded) may be used as
// one path segment: no separators, no dot segments, no control characters.
func ValidSegment(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 320 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == '?' || r == '#' {
			return false
		}
	}
	return true
}

// Match reports whether method and segments fit one route such as
// "GET mailboxes/*/emails".
func Match(route, method string, segments []string) bool {
	m, pattern, ok := strings.Cut(route, " ")
	if !ok || m != method {
		return false
	}
	parts := strings.Split(pattern, "/")
	if len(parts) != len(segments) {
		return false
	}
	for i, p := range parts {
		if !ValidSegment(segments[i]) || (p != "*" && p != segments[i]) {
			return false
		}
	}
	return true
}

// AdminAllowed reports whether the request is one of AdminRoutes.
func AdminAllowed(method string, segments []string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
	default:
		return false
	}
	for _, r := range AdminRoutes {
		if Match(r, method, segments) {
			return true
		}
	}
	return false
}

package server

import (
	"errors"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"xauth/internal/password"
	"xauth/internal/store"
)

func validEmail(raw string) (string, bool) {
	email := store.NormalizeEmail(raw)
	if email == "" || len(email) > 254 {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" {
		return "", false
	}
	return email, true
}

// cleanName trims a display name and checks it is at most max characters
// without control characters. min is the minimum length after trimming.
func cleanName(raw string, min, max int) (string, bool) {
	name := strings.TrimSpace(raw)
	n := utf8.RuneCountInString(name)
	if n < min || n > max {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}

// passwordError maps a password policy error to an API error code.
func passwordError(err error) string {
	if errors.Is(err, password.ErrTooLong) {
		return "password_too_long"
	}
	return "password_too_short"
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateRedirectURIs trims, de-duplicates and checks a list of redirect
// URIs for a client type. It returns a reason in Chinese on failure.
func validateRedirectURIs(list []string, clientType string, dev, required bool) ([]string, string) {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, raw := range list {
		u := strings.TrimSpace(raw)
		if u == "" || seen[u] {
			continue
		}
		if reason := checkRedirectURI(u, clientType, dev); reason != "" {
			return nil, u + "：" + reason
		}
		seen[u] = true
		out = append(out, u)
	}
	if required && len(out) == 0 {
		return nil, "至少需要一个回调地址。"
	}
	if len(out) > 20 {
		return nil, "回调地址最多 20 个。"
	}
	return out, ""
}

func checkRedirectURI(raw, clientType string, dev bool) string {
	if len(raw) > 2048 {
		return "地址过长。"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "不是完整的 URL。"
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return "不能包含 # 片段。"
	}
	if u.User != nil {
		return "不能包含用户名或密码。"
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "https":
		if u.Host == "" {
			return "缺少主机名。"
		}
		return ""
	case "http":
		if u.Host == "" {
			return "缺少主机名。"
		}
		switch clientType {
		case store.ClientWeb:
			return ""
		case store.ClientSPA:
			if dev {
				return ""
			}
			return "单页应用必须使用 https（开发模式除外）。"
		default:
			if isLoopbackHost(u.Hostname()) {
				return ""
			}
			return "原生应用只能对 localhost 或 127.0.0.1 使用 http。"
		}
	case "javascript", "data", "file", "vbscript", "blob", "about":
		return "不允许使用该协议。"
	default:
		if clientType != store.ClientNative {
			return "Web 应用和单页应用只能使用 http 或 https。"
		}
		return ""
	}
}

// Package outlook reads Outlook/Hotmail mailboxes via Microsoft Graph API and
// OAuth2 IMAP (outlook.live.com / outlook.office365.com), and parses batch
// account import lines of the form "email----password----client_id----refresh_token".
package outlook

import (
	"fmt"
	"strings"
)

// ParsedAccount is one validated account line from a batch import.
type ParsedAccount struct {
	Line         int    `json:"line"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	ClientID     string `json:"clientId"`
	RefreshToken string `json:"refreshToken"`
}

// ParseError describes a line that could not be parsed during batch import.
type ParseError struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// NormalizeEmail trims and lowercases an email address.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidEmail checks the basic shape of an email address.
func ValidEmail(addr string) bool {
	local, domain, ok := strings.Cut(addr, "@")
	if !ok || local == "" || domain == "" || len(addr) > 254 {
		return false
	}
	if strings.Contains(domain, "@") || !strings.Contains(domain, ".") {
		return false
	}
	if strings.ContainsAny(addr, " \t\r\n\x00/\\") {
		return false
	}
	return true
}

// ParseAccountLine parses a single formatted account string:
//   - 4-part standard format: email----password----client_id----refresh_token
//   - 3-part format (no password): email----client_id----refresh_token
func ParseAccountLine(line string) (ParsedAccount, error) {
	trimmed := strings.TrimSpace(line)
	parts := strings.Split(trimmed, "----")
	var email, password, clientID, refreshToken string
	switch {
	case len(parts) >= 4:
		email = NormalizeEmail(parts[0])
		password = strings.TrimSpace(parts[1])
		clientID = strings.TrimSpace(parts[2])
		refreshToken = strings.TrimSpace(strings.Join(parts[3:], "----"))
	case len(parts) == 3:
		email = NormalizeEmail(parts[0])
		clientID = strings.TrimSpace(parts[1])
		refreshToken = strings.TrimSpace(parts[2])
	default:
		return ParsedAccount{}, fmt.Errorf("格式应为：邮箱----密码----client_id----refresh_token")
	}
	if !ValidEmail(email) {
		return ParsedAccount{}, fmt.Errorf("邮箱地址无效：%s", email)
	}
	if clientID == "" {
		return ParsedAccount{}, fmt.Errorf("client_id 不能为空")
	}
	if refreshToken == "" {
		return ParsedAccount{}, fmt.Errorf("refresh_token 不能为空")
	}
	if strings.ContainsAny(clientID, " \t\r\n\x00") || strings.ContainsAny(refreshToken, " \t\r\n\x00") {
		return ParsedAccount{}, fmt.Errorf("client_id 或 refresh_token 包含非法空白字符")
	}
	return ParsedAccount{
		Email:        email,
		Password:     password,
		ClientID:     clientID,
		RefreshToken: refreshToken,
	}, nil
}

// ParseAccountsText parses a multi-line string of formatted Outlook accounts.
// Blank lines are ignored. Duplicate emails within the same batch keep the
// last occurrence.
func ParseAccountsText(text string) ([]ParsedAccount, []ParseError) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var accounts []ParsedAccount
	var errs []ParseError
	seenIndex := map[string]int{}

	for i, raw := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		acc, err := ParseAccountLine(trimmed)
		if err != nil {
			preview := trimmed
			if len(preview) > 64 {
				preview = preview[:64] + "..."
			}
			errs = append(errs, ParseError{
				Line:   lineNum,
				Raw:    preview,
				Reason: err.Error(),
			})
			continue
		}
		acc.Line = lineNum
		if idx, exists := seenIndex[acc.Email]; exists {
			accounts[idx] = acc
		} else {
			seenIndex[acc.Email] = len(accounts)
			accounts = append(accounts, acc)
		}
	}
	return accounts, errs
}

// FormatAccountLine formats an account back to "email----password----client_id----refresh_token".
func FormatAccountLine(email, password, clientID, refreshToken string) string {
	return fmt.Sprintf("%s----%s----%s----%s", email, password, clientID, refreshToken)
}

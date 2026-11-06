package outlook

import (
	"regexp"
	"strings"
)

var (
	reHTMLTag = regexp.MustCompile(`(?s)<style[^>]*>.*?</style>|<script[^>]*>.*?</script>|<[^>]+>`)
	reSpaces  = regexp.MustCompile(`\s+`)

	// Contextual patterns where a keyword precedes or follows a 4-8 character code.
	reContextBefore    = regexp.MustCompile(`(?i)(?:验证码|安全代码|安全码|动态码|校验码|确认码|一次性密码|一次性代码|认证码|验证代码|登录码|代码为|码为|code(?:\s+is)?|verification\s+code|security\s+code|confirmation\s+code|one-time\s+(?:password|code)|passcode|otp|pin)[^0-9a-zA-Z]{0,24}?([0-9]{4,8}|[0-9A-Z]{6})\b`)
	reContextAfter     = regexp.MustCompile(`(?i)\b([0-9]{4,8})\s*(?:是您的|是你的|为您的|为你的|is\s+your)\s*(?:验证码|安全代码|安全码|动态码|校验码|确认码|verification\s+code|security\s+code|confirmation\s+code|one-time\s+code|code|otp)`)
	reDigitsStandalone = regexp.MustCompile(`\b([0-9]{6})\b`)

	reTriggerWord = regexp.MustCompile(`(?i)(验证码|安全代码|安全码|动态码|校验码|确认码|一次性|认证码|verification|security\s+code|passcode|one-time|otp|verify\s+your)`)
)

// StripHTML converts HTML markup into plain text suitable for previews and code extraction.
func StripHTML(htmlStr string) string {
	if htmlStr == "" {
		return ""
	}
	s := reHTMLTag.ReplaceAllString(htmlStr, " ")
	r := strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
	)
	s = r.Replace(s)
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// ExtractCode attempts to extract a 4-8 digit (or 6-char alphanumeric) verification
// code from an email's subject and body. Returns "" when no code is found.
func ExtractCode(subject, text, htmlBody string) string {
	plain := strings.TrimSpace(text)
	if plain == "" && htmlBody != "" {
		plain = StripHTML(htmlBody)
	}
	combined := subject + "\n" + plain

	for _, src := range []string{subject, combined} {
		if src == "" {
			continue
		}
		if matches := reContextBefore.FindAllStringSubmatch(src, -1); len(matches) > 0 {
			for _, m := range matches {
				if isValidCandidate(m[1]) {
					return m[1]
				}
			}
		}
		if matches := reContextAfter.FindAllStringSubmatch(src, -1); len(matches) > 0 {
			for _, m := range matches {
				if isValidCandidate(m[1]) {
					return m[1]
				}
			}
		}
	}

	// Fallback: if the email clearly looks like a verification email, look for a 6-digit number.
	if reTriggerWord.MatchString(combined) {
		for _, m := range reDigitsStandalone.FindAllStringSubmatch(combined, -1) {
			if isValidCandidate(m[1]) {
				return m[1]
			}
		}
	}
	return ""
}

func isValidCandidate(c string) bool {
	if len(c) < 4 || len(c) > 8 {
		return false
	}
	// Reject pure letters (e.g. "OFFICE", "GOOGLE" matched by [0-9A-Z]{6}).
	hasDigit := false
	for _, r := range c {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return false
	}
	// Reject 4-digit years 1990..2039 unless 6+ digits.
	if len(c) == 4 && (strings.HasPrefix(c, "199") || strings.HasPrefix(c, "200") || strings.HasPrefix(c, "201") || strings.HasPrefix(c, "202") || strings.HasPrefix(c, "203")) {
		return false
	}
	return true
}

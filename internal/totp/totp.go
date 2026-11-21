// Package totp parses otpauth secrets and generates TOTP codes.
// Codes are computed on the server; the browser never receives the secret.
package totp

import (
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	openotp "github.com/pquerna/otp/totp"
)

// Algorithms accepted by RFC 6238.
const (
	SHA1   = "SHA1"
	SHA256 = "SHA256"
	SHA512 = "SHA512"
)

const (
	defaultDigits = 6
	defaultPeriod = 30
	// Bound the UI refresh interval while preserving nonstandard OTP periods.
	maxPeriod = 86400
)

// ErrInvalid describes a secret or otpauth URL the user can correct.
var ErrInvalid = errors.New("invalid totp input")

// Entry is a normalized authenticator entry.
type Entry struct {
	Secret      string
	Name        string
	Issuer      string
	AccountName string
	Digits      int
	Period      int
	Algorithm   string
	Note        string
}

// Parse accepts a Base32 secret or an otpauth://totp URL.
// Fields in overrides replace values parsed from the URL.
func Parse(input string, overrides Entry) (Entry, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" && strings.TrimSpace(overrides.Secret) == "" {
		return Entry{}, fmt.Errorf("%w: empty", ErrInvalid)
	}
	var parsed Entry
	if strings.HasPrefix(strings.ToLower(trimmed), "otpauth://") {
		var err error
		parsed, err = parseURL(trimmed)
		if err != nil {
			return Entry{}, err
		}
	} else if trimmed != "" {
		secret, err := normalizeSecret(trimmed)
		if err != nil {
			return Entry{}, err
		}
		parsed.Secret = secret
	}
	if overrides.Secret != "" {
		secret, err := normalizeSecret(overrides.Secret)
		if err != nil {
			return Entry{}, err
		}
		parsed.Secret = secret
	}
	if parsed.Secret == "" {
		return Entry{}, fmt.Errorf("%w: missing secret", ErrInvalid)
	}
	if overrides.Issuer != "" {
		parsed.Issuer = strings.TrimSpace(overrides.Issuer)
	}
	if overrides.AccountName != "" {
		parsed.AccountName = strings.TrimSpace(overrides.AccountName)
	}
	if overrides.Digits != 0 {
		parsed.Digits = overrides.Digits
	}
	if overrides.Period != 0 {
		parsed.Period = overrides.Period
	}
	if overrides.Algorithm != "" {
		parsed.Algorithm = overrides.Algorithm
	}
	if err := normalizeParameters(&parsed); err != nil {
		return Entry{}, err
	}
	parsed.Note = strings.TrimSpace(firstText(overrides.Note, parsed.Note))
	parsed.Name = displayName(parsed.Issuer, parsed.AccountName, overrides.Name)
	return parsed, nil
}

func parseURL(raw string) (Entry, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "otpauth") {
		return Entry{}, fmt.Errorf("%w: bad url", ErrInvalid)
	}
	if !strings.EqualFold(u.Host, "totp") || u.User != nil || u.Fragment != "" {
		return Entry{}, fmt.Errorf("%w: only totp", ErrInvalid)
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: bad query", ErrInvalid)
	}
	for _, name := range []string{"secret", "issuer", "digits", "period", "algorithm", "encoder"} {
		if len(q[name]) > 1 {
			return Entry{}, fmt.Errorf("%w: duplicate %s", ErrInvalid, name)
		}
	}
	if q.Get("encoder") != "" {
		return Entry{}, fmt.Errorf("%w: unsupported encoder", ErrInvalid)
	}
	secret := q.Get("secret")
	if secret == "" {
		return Entry{}, fmt.Errorf("%w: missing secret", ErrInvalid)
	}
	normalized, err := normalizeSecret(secret)
	if err != nil {
		return Entry{}, err
	}
	// url.Parse has already unescaped Path; decoding it twice changes labels.
	label := strings.TrimLeft(u.Path, "/")
	var issuer, account string
	if label != "" {
		if head, tail, ok := strings.Cut(label, ":"); ok {
			issuer = strings.TrimSpace(head)
			account = strings.TrimSpace(tail)
		} else {
			account = strings.TrimSpace(label)
		}
	}
	if value := strings.TrimSpace(q.Get("issuer")); value != "" {
		issuer = value
	}
	digits, err := urlNumber(q, "digits", defaultDigits)
	if err != nil {
		return Entry{}, err
	}
	period, err := urlNumber(q, "period", defaultPeriod)
	if err != nil {
		return Entry{}, err
	}
	parsed := Entry{
		Secret: normalized, Issuer: issuer, AccountName: account,
		Digits: digits, Period: period, Algorithm: q.Get("algorithm"),
	}
	// Validate the URL before applying overrides: a bad link must never turn
	// into a plausible-looking entry just because the form sent defaults.
	if err := normalizeParameters(&parsed); err != nil {
		return Entry{}, err
	}
	return parsed, nil
}

func urlNumber(q url.Values, name string, fallback int) (int, error) {
	if !q.Has(name) {
		return fallback, nil
	}
	v, err := strconv.ParseUint(q.Get(name), 10, 32)
	if err != nil || v == 0 {
		return 0, fmt.Errorf("%w: %s", ErrInvalid, name)
	}
	return int(v), nil
}

func normalizeSecret(secret string) (string, error) {
	s := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "", "\n", "", "\r", "").Replace(secret))
	s = strings.TrimRight(s, "=")
	if s == "" {
		return "", fmt.Errorf("%w: empty secret", ErrInvalid)
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) {
			return "", fmt.Errorf("%w: secret", ErrInvalid)
		}
	}
	if _, err := decodeBase32(s); err != nil {
		return "", err
	}
	return s, nil
}

func decodeBase32(secret string) ([]byte, error) {
	// Pad so the standard decoder accepts secrets that omit '='.
	if pad := len(secret) % 8; pad != 0 {
		secret += strings.Repeat("=", 8-pad)
	}
	b, err := base32.StdEncoding.DecodeString(secret)
	if err != nil || len(b) == 0 {
		return nil, fmt.Errorf("%w: secret", ErrInvalid)
	}
	return b, nil
}

func normalizeParameters(e *Entry) error {
	if e.Digits == 0 {
		e.Digits = defaultDigits
	}
	if e.Period == 0 {
		e.Period = defaultPeriod
	}
	if e.Digits < 6 || e.Digits > 8 || e.Period < 1 || e.Period > maxPeriod {
		return fmt.Errorf("%w: digits or period", ErrInvalid)
	}
	e.Algorithm = strings.ToUpper(strings.TrimSpace(e.Algorithm))
	if e.Algorithm == "" {
		e.Algorithm = SHA1
	}
	if e.Algorithm != SHA1 && e.Algorithm != SHA256 && e.Algorithm != SHA512 {
		return fmt.Errorf("%w: algorithm", ErrInvalid)
	}
	return nil
}

func displayName(issuer, account, fallback string) string {
	if name := strings.TrimSpace(fallback); name != "" {
		return name
	}
	if issuer != "" && account != "" {
		return issuer + " (" + account + ")"
	}
	if issuer != "" {
		return issuer
	}
	if account != "" {
		return account
	}
	return "未命名验证器"
}

func firstText(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Code returns the code for entry at the given time.
func Code(entry Entry, now time.Time) (string, error) {
	secret, err := normalizeSecret(entry.Secret)
	if err != nil {
		return "", err
	}
	if err := normalizeParameters(&entry); err != nil {
		return "", err
	}
	algorithm := otp.AlgorithmSHA1
	switch entry.Algorithm {
	case SHA256:
		algorithm = otp.AlgorithmSHA256
	case SHA512:
		algorithm = otp.AlgorithmSHA512
	}
	return openotp.GenerateCodeCustom(secret, now, openotp.ValidateOpts{
		Period: uint(entry.Period), Digits: otp.Digits(entry.Digits), Algorithm: algorithm,
	})
}

// ExpiresAt returns the exact end of the current time step.
func ExpiresAt(period int, now time.Time) time.Time {
	if period <= 0 {
		period = defaultPeriod
	}
	return time.Unix((now.Unix()/int64(period)+1)*int64(period), 0)
}

// RemainingSeconds is how long the current code stays valid.
func RemainingSeconds(period int, now time.Time) int {
	return int((ExpiresAt(period, now).Sub(now) + time.Second - 1) / time.Second)
}

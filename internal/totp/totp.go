// Package totp parses otpauth secrets and generates TOTP codes.
// Codes are computed on the server; the browser never receives the secret.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	parsed.Digits = normalizeDigits(firstPositive(overrides.Digits, parsed.Digits))
	parsed.Period = normalizePeriod(firstPositive(overrides.Period, parsed.Period))
	parsed.Algorithm = normalizeAlgorithm(firstText(overrides.Algorithm, parsed.Algorithm))
	parsed.Note = strings.TrimSpace(firstText(overrides.Note, parsed.Note))
	parsed.Name = displayName(parsed.Issuer, parsed.AccountName, overrides.Name)
	return parsed, nil
}

func parseURL(raw string) (Entry, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "otpauth") {
		return Entry{}, fmt.Errorf("%w: bad url", ErrInvalid)
	}
	if !strings.EqualFold(u.Host, "totp") {
		return Entry{}, fmt.Errorf("%w: only totp", ErrInvalid)
	}
	secret := u.Query().Get("secret")
	if secret == "" {
		return Entry{}, fmt.Errorf("%w: missing secret", ErrInvalid)
	}
	normalized, err := normalizeSecret(secret)
	if err != nil {
		return Entry{}, err
	}
	label, err := url.PathUnescape(strings.TrimPrefix(u.Path, "/"))
	if err != nil {
		label = strings.TrimPrefix(u.EscapedPath(), "/")
	}
	var issuer, account string
	if label != "" {
		if head, tail, ok := strings.Cut(label, ":"); ok {
			issuer = strings.TrimSpace(head)
			account = strings.TrimSpace(tail)
		} else {
			account = strings.TrimSpace(label)
		}
	}
	if q := strings.TrimSpace(u.Query().Get("issuer")); q != "" {
		issuer = q
	}
	digits, _ := strconv.Atoi(u.Query().Get("digits"))
	period, _ := strconv.Atoi(u.Query().Get("period"))
	return Entry{
		Secret: normalized, Issuer: issuer, AccountName: account,
		Digits: digits, Period: period, Algorithm: u.Query().Get("algorithm"),
	}, nil
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

func normalizeDigits(v int) int {
	if v < 6 || v > 8 {
		return defaultDigits
	}
	return v
}

func normalizePeriod(v int) int {
	if v < 15 || v > 120 {
		return defaultPeriod
	}
	return v
}

func normalizeAlgorithm(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case SHA256:
		return SHA256
	case SHA512:
		return SHA512
	default:
		return SHA1
	}
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

func firstPositive(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

func firstText(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Code returns the code for entry at the given time.
func Code(entry Entry, now time.Time) (string, error) {
	key, err := decodeBase32(entry.Secret)
	if err != nil {
		return "", err
	}
	digits := normalizeDigits(entry.Digits)
	period := normalizePeriod(entry.Period)
	counter := uint64(now.Unix()) / uint64(period)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	var h func() hash.Hash
	switch normalizeAlgorithm(entry.Algorithm) {
	case SHA256:
		h = sha256.New
	case SHA512:
		h = sha512.New
	default:
		h = sha1.New
	}
	mac := hmac.New(h, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod), nil
}

// RemainingSeconds is how long the current code stays valid.
func RemainingSeconds(period int, now time.Time) int {
	period = normalizePeriod(period)
	left := period - int(now.Unix()%int64(period))
	if left == 0 {
		return period
	}
	return left
}

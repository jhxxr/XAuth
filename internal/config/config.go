// Package config loads XAuth settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime settings. Durations have fixed defaults; only the
// values that differ between deployments are read from the environment.
type Config struct {
	// Issuer is the public base URL of XAuth, e.g. https://auth.example.com.
	// It must not have a path, query or trailing slash.
	Issuer string
	// Listen is the TCP address the HTTP server binds to.
	Listen string
	// DBPath is the SQLite database file.
	DBPath string
	// Dev relaxes issuer and redirect URI checks so http:// works locally.
	Dev bool
	// SignupEnabled lets anyone create an account on the login page.
	SignupEnabled bool
	// TrustProxy makes rate limiting use X-Forwarded-For / X-Real-IP.
	// Enable it only behind a reverse proxy that overwrites those headers.
	TrustProxy bool
	// XMailBaseURL is the xmail deployment, e.g. https://mail.example.com,
	// without /api/v1. Empty means mail features are off.
	XMailBaseURL string
	// XMailAPIKey is xmail's sk_live_ key. It only ever leaves the process
	// in the Authorization header sent to XMailBaseURL.
	XMailAPIKey Secret
	// SMTP sends verification and password-reset mail. It is not xmail.
	SMTP SMTPConfig

	SessionTTL      time.Duration
	AuthRequestTTL  time.Duration
	AuthCodeTTL     time.Duration
	AccessTokenTTL  time.Duration
	IDTokenTTL      time.Duration
	RefreshTokenTTL time.Duration
}

// Defaults returns a config with every field set to its default value.
func Defaults() Config {
	return Config{
		Issuer:          "http://localhost:8080",
		Listen:          "127.0.0.1:8080",
		DBPath:          "data/xauth.db",
		SignupEnabled:   true,
		SessionTTL:      14 * 24 * time.Hour,
		AuthRequestTTL:  30 * time.Minute,
		AuthCodeTTL:     2 * time.Minute,
		AccessTokenTTL:  time.Hour,
		IDTokenTTL:      time.Hour,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
}

// FromEnv reads XAUTH_* variables on top of the defaults.
func FromEnv() (Config, error) {
	c := Defaults()
	if v := os.Getenv("XAUTH_ISSUER"); v != "" {
		c.Issuer = v
	}
	if v := os.Getenv("XAUTH_LISTEN"); v != "" {
		c.Listen = v
	}
	if v := os.Getenv("XAUTH_DB"); v != "" {
		c.DBPath = v
	}
	var err error
	if c.Dev, err = envBool("XAUTH_DEV", c.Dev); err != nil {
		return c, err
	}
	if c.SignupEnabled, err = envBool("XAUTH_SIGNUP", c.SignupEnabled); err != nil {
		return c, err
	}
	if c.TrustProxy, err = envBool("XAUTH_TRUST_PROXY", c.TrustProxy); err != nil {
		return c, err
	}
	c.XMailBaseURL = os.Getenv("XMAIL_BASE_URL")
	c.XMailAPIKey = Secret(strings.TrimSpace(os.Getenv("XMAIL_API_KEY")))
	c.SMTP = SMTPConfig{
		Host: strings.TrimSpace(os.Getenv("XAUTH_SMTP_HOST")),
		From: strings.TrimSpace(os.Getenv("XAUTH_SMTP_FROM")),
		User: os.Getenv("XAUTH_SMTP_USER"),
		Pass: Secret(os.Getenv("XAUTH_SMTP_PASS")),
	}
	if v := strings.TrimSpace(os.Getenv("XAUTH_SMTP_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return c, fmt.Errorf("XAUTH_SMTP_PORT=%q is not a port", v)
		}
		c.SMTP.Port = port
	}
	if c.SMTP.TLS, err = envBool("XAUTH_SMTP_TLS", true); err != nil {
		return c, err
	}
	return c, c.Validate()
}

// Secret is a string that does not print itself, so a config dumped into a
// log line or error message cannot leak it.
type Secret string

// String implements fmt.Stringer.
func (Secret) String() string { return "[redacted]" }

// GoString implements fmt.GoStringer for %#v.
func (Secret) GoString() string { return `"[redacted]"` }

// MarshalJSON keeps the value out of JSON output.
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

// Reveal returns the actual value. Only the xmail client should call it.
func (s Secret) Reveal() string { return string(s) }

// MailEnabled reports whether xmail is configured.
func (c Config) MailEnabled() bool { return c.XMailBaseURL != "" }

// SMTPConfig is the outbound mail server used for verification and resets.
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass Secret
	From string
	TLS  bool
}

// Enabled reports whether verification mail can be sent.
func (s SMTPConfig) Enabled() bool { return s.Host != "" && s.From != "" }

// Addr is the host:port dialed by the mail client.
func (s SMTPConfig) Addr() string {
	port := s.Port
	if port == 0 {
		if s.TLS {
			port = 587
		} else {
			port = 25
		}
	}
	return fmt.Sprintf("%s:%d", s.Host, port)
}

// Validate checks the issuer URL and normalises it.
func (c *Config) Validate() error {
	if err := c.validateXMail(); err != nil {
		return err
	}
	if err := c.validateSMTP(); err != nil {
		return err
	}
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Host == "" {
		return fmt.Errorf("XAUTH_ISSUER %q is not an absolute URL", c.Issuer)
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("XAUTH_ISSUER %q must be scheme://host[:port] with no path, query or credentials", c.Issuer)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !c.Dev {
			return errors.New("XAUTH_ISSUER uses http://; set XAUTH_DEV=true for local development or use https://")
		}
	default:
		return fmt.Errorf("XAUTH_ISSUER scheme %q is not http or https", u.Scheme)
	}
	return nil
}

// validateXMail normalises XMAIL_BASE_URL and requires both variables or
// neither. A trailing /api/v1 is tolerated and removed.
func (c *Config) validateXMail() error {
	base := strings.TrimRight(strings.TrimSpace(c.XMailBaseURL), "/")
	base = strings.TrimRight(strings.TrimSuffix(base, "/api/v1"), "/")
	c.XMailBaseURL = base
	switch {
	case base == "" && c.XMailAPIKey == "":
		return nil
	case base == "":
		return errors.New("XMAIL_API_KEY is set but XMAIL_BASE_URL is not")
	case c.XMailAPIKey == "":
		return errors.New("XMAIL_BASE_URL is set but XMAIL_API_KEY is not")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("XMAIL_BASE_URL %q must be an absolute URL such as https://mail.example.com", base)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !c.Dev {
			return errors.New("XMAIL_BASE_URL uses http://; the API key would travel in clear text. Use https:// (or XAUTH_DEV=true locally)")
		}
	default:
		return fmt.Errorf("XMAIL_BASE_URL scheme %q is not http or https", u.Scheme)
	}
	return nil
}

// validateSMTP requires a host and a from address together, and checks that
// the from address is a bare mailbox.
func (c *Config) validateSMTP() error {
	s := &c.SMTP
	s.Host = strings.TrimSpace(s.Host)
	s.From = strings.TrimSpace(s.From)
	switch {
	case s.Host == "" && s.From == "":
		return nil
	case s.Host == "":
		return errors.New("XAUTH_SMTP_FROM is set but XAUTH_SMTP_HOST is not")
	case s.From == "":
		return errors.New("XAUTH_SMTP_HOST is set but XAUTH_SMTP_FROM is not")
	}
	addr, err := mail.ParseAddress(s.From)
	if err != nil || addr.Name != "" || addr.Address != s.From {
		return fmt.Errorf("XAUTH_SMTP_FROM %q must be a bare address such as XAuth <no-reply@example.com>", s.From)
	}
	if s.User != "" && s.Pass == "" {
		return errors.New("XAUTH_SMTP_USER is set but XAUTH_SMTP_PASS is not")
	}
	return nil
}

// SecureCookies reports whether cookies should carry the Secure attribute.
func (c Config) SecureCookies() bool {
	return strings.HasPrefix(c.Issuer, "https://")
}

func envBool(key string, def bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s=%q is not a boolean", key, v)
	}
	return b, nil
}

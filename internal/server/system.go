package server

import (
	"context"
	"encoding/json"
	"net/mail"
	"net/url"
	"strings"
	"unicode"

	"xauth/internal/config"
	"xauth/internal/mailer"
	"xauth/internal/store"
	"xauth/internal/xmail"
)

// storedSMTP is the JSON kept in the settings table. It is not config.SMTPConfig:
// that type redacts its password when marshalled.
type storedSMTP struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
	Pass string `json:"pass"`
	From string `json:"from"`
	TLS  bool   `json:"tls"`
}

func (s storedSMTP) config() config.SMTPConfig {
	return config.SMTPConfig{
		Host: s.Host, Port: s.Port, User: s.User, Pass: config.Secret(s.Pass), From: s.From, TLS: s.TLS,
	}
}

func storedFrom(cfg config.SMTPConfig) storedSMTP {
	return storedSMTP{
		Host: cfg.Host, Port: cfg.Port, User: cfg.User, Pass: cfg.Pass.Reveal(), From: cfg.From, TLS: cfg.TLS,
	}
}

// loadStoredSMTP applies an admin-saved mail server, if one exists.
// A broken value is ignored so a bad write cannot stop the process.
func (s *Server) loadStoredSMTP(ctx context.Context) {
	raw, ok, err := s.st.ReadSetting(ctx, store.SettingSMTP)
	if err != nil {
		s.log.Error("read smtp settings", "err", err)
		return
	}
	if !ok {
		return
	}
	var stored storedSMTP
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		s.log.Error("smtp settings ignored", "err", err)
		return
	}
	s.applySMTP(stored.config(), true)
}

// applySMTP swaps the live mail server. fromDB is false when control
// returns to the environment captured at startup.
func (s *Server) applySMTP(cfg config.SMTPConfig, fromDB bool) {
	s.mu.Lock()
	s.cfg.SMTP = cfg
	s.smtpFromDB = fromDB
	s.mailer = mailer.New(cfg)
	s.mu.Unlock()
}

func (s *Server) mailEnabled() bool {
	cfg, _, _ := s.mailSnapshot()
	return cfg.Enabled()
}

func (s *Server) currentMailer() mailer.Sender {
	_, _, m := s.mailSnapshot()
	return m
}

func (s *Server) mailSnapshot() (config.SMTPConfig, bool, mailer.Sender) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.SMTP, s.smtpFromDB, s.mailer
}

type smtpJSON struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	From        string `json:"from"`
	TLS         bool   `json:"tls"`
	PasswordSet bool   `json:"passwordSet"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"` // "database" or "environment"
}

func (s *Server) smtpView() smtpJSON {
	cfg, fromDB, _ := s.mailSnapshot()
	port := cfg.Port
	if port == 0 {
		if cfg.TLS || cfg.Host == "" {
			port = 587
		} else {
			port = 25
		}
	}
	source := "environment"
	if fromDB {
		source = "database"
	}
	tls := cfg.TLS || cfg.Host == ""
	return smtpJSON{
		Host: cfg.Host, Port: port, User: cfg.User, From: cfg.From, TLS: tls,
		PasswordSet: cfg.Pass != "", Enabled: cfg.Enabled(), Source: source,
	}
}

type smtpUpdate struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	User          string `json:"user"`
	Password      string `json:"password"`
	ClearPassword bool   `json:"clearPassword"`
	From          string `json:"from"`
	TLS           bool   `json:"tls"`
}

// proposeSMTP checks an admin edit against the mail server currently in
// effect. An empty host and from address disables mail. The returned config
// is what should be stored; code is an API error code, or "" when it is valid.
func proposeSMTP(cur config.SMTPConfig, upd smtpUpdate) (config.SMTPConfig, string) {
	host := strings.TrimSpace(upd.Host)
	from := strings.TrimSpace(upd.From)
	user := strings.TrimSpace(upd.User)
	if host == "" && from == "" {
		return config.SMTPConfig{}, ""
	}
	if host == "" || from == "" || !validSMTPHost(host) {
		return config.SMTPConfig{}, "invalid_smtp"
	}
	if upd.Port < 0 || upd.Port > 65535 {
		return config.SMTPConfig{}, "invalid_smtp_port"
	}
	addr, err := mail.ParseAddress(from)
	if err != nil || addr.Name != "" || addr.Address != from {
		return config.SMTPConfig{}, "invalid_smtp_from"
	}
	pass := cur.Pass.Reveal()
	switch {
	case upd.ClearPassword:
		pass = ""
	case upd.Password != "":
		pass = upd.Password
	}
	if user != "" && pass == "" {
		return config.SMTPConfig{}, "smtp_password_required"
	}
	return config.SMTPConfig{
		Host: host, Port: upd.Port, User: user, Pass: config.Secret(pass), From: from, TLS: upd.TLS,
	}, ""
}

func validSMTPHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	// The port has its own field, so a colon here would be a second port
	// or a URL. IPv6 literals are not accepted.
	if strings.ContainsAny(host, " /:@\\") {
		return false
	}
	for _, r := range host {
		if r > 127 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// saveSMTP stores cfg and starts using it immediately.
func (s *Server) saveSMTP(ctx context.Context, cfg config.SMTPConfig) error {
	raw, err := json.Marshal(storedFrom(cfg))
	if err != nil {
		return err
	}
	if err := s.st.WriteSetting(ctx, store.SettingSMTP, string(raw)); err != nil {
		return err
	}
	s.applySMTP(cfg, true)
	return nil
}

// resetSMTP forgets the saved mail server and returns to the environment.
func (s *Server) resetSMTP(ctx context.Context) error {
	if err := s.st.DeleteSetting(ctx, store.SettingSMTP); err != nil {
		return err
	}
	s.applySMTP(s.envSMTP, false)
	return nil
}

// storedXMail is the JSON kept for the xmail deployment. The API key is
// never marshalled with config.Secret, which would redact it.
type storedXMail struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

func (s *Server) loadStoredXMail(ctx context.Context) {
	raw, ok, err := s.st.ReadSetting(ctx, store.SettingXMail)
	if err != nil {
		s.log.Error("read xmail settings", "err", err)
		return
	}
	if !ok {
		return
	}
	var stored storedXMail
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		s.log.Error("xmail settings ignored", "err", err)
		return
	}
	s.applyXMail(stored.BaseURL, stored.APIKey, true)
}

// applyXMail swaps the live xmail client. An empty address disables it.
// The previous client is left for requests that already hold it.
func (s *Server) applyXMail(base, key string, fromDB bool) {
	var c *xmail.Client
	if base != "" && key != "" {
		client, err := xmail.New(base, key)
		if err != nil {
			s.log.Error("xmail disabled", "err", err)
		} else {
			c = client
		}
	}
	s.mu.Lock()
	s.mail = c
	s.xmailBase = base
	s.xmailKey = key
	s.xmailFromDB = fromDB
	s.mu.Unlock()
}

// xmailClient is the client to use for one request. The pointer stays
// valid if an admin swaps the deployment while the call is in flight.
func (s *Server) xmailClient() *xmail.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mail
}

type xmailJSON struct {
	BaseURL string `json:"baseUrl"`
	KeySet  bool   `json:"keySet"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

func (s *Server) xmailView() xmailJSON {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source := "environment"
	if s.xmailFromDB {
		source = "database"
	}
	return xmailJSON{
		BaseURL: s.xmailBase, KeySet: s.xmailKey != "", Enabled: s.mail != nil, Source: source,
	}
}

type xmailUpdate struct {
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	ClearKey bool   `json:"clearKey"`
}

// proposeXMail checks an admin edit. An empty address disables xmail.
// code is an API error code, or "" when the result should be stored.
func (s *Server) proposeXMail(upd xmailUpdate) (base, key, code string) {
	base, code = normalizeXMailBase(upd.BaseURL, s.cfg.Dev)
	if code != "" {
		return "", "", code
	}
	s.mu.RLock()
	curKey := s.xmailKey
	s.mu.RUnlock()
	if base == "" {
		if strings.TrimSpace(upd.APIKey) != "" {
			return "", "", "invalid_xmail_url"
		}
		return "", "", ""
	}
	switch {
	case upd.ClearKey:
		key = ""
	case strings.TrimSpace(upd.APIKey) != "":
		key = strings.TrimSpace(upd.APIKey)
	default:
		key = curKey
	}
	if key == "" {
		return "", "", "xmail_key_required"
	}
	return base, key, ""
}

func normalizeXMailBase(raw string, dev bool) (string, string) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	base = strings.TrimRight(strings.TrimSuffix(base, "/api/v1"), "/")
	if base == "" {
		return "", ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return "", "invalid_xmail_url"
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !dev {
			return "", "xmail_insecure"
		}
	default:
		return "", "invalid_xmail_url"
	}
	return base, ""
}

func (s *Server) saveXMail(ctx context.Context, base, key string) error {
	raw, err := json.Marshal(storedXMail{BaseURL: base, APIKey: key})
	if err != nil {
		return err
	}
	if err := s.st.WriteSetting(ctx, store.SettingXMail, string(raw)); err != nil {
		return err
	}
	s.applyXMail(base, key, true)
	return nil
}

func (s *Server) resetXMail(ctx context.Context) error {
	if err := s.st.DeleteSetting(ctx, store.SettingXMail); err != nil {
		return err
	}
	s.applyXMail(s.envXMailURL, s.envXMailKey, false)
	return nil
}

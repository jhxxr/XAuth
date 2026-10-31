// Package mailer sends XAuth's own verification and reset messages.
package mailer

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"xauth/internal/config"
)

// Message is one outbound mail.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender delivers a message. Tests replace it.
type Sender interface {
	Send(Message) error
}

// SMTP sends through the configured server.
type SMTP struct {
	cfg config.SMTPConfig
}

// New returns a sender, or nil when outbound mail is not configured.
func New(cfg config.SMTPConfig) Sender {
	if !cfg.Enabled() {
		return nil
	}
	return SMTP{cfg: cfg}
}

// Send delivers one plain-text message.
func (s SMTP) Send(m Message) error {
	body := strings.ReplaceAll(m.Text, "\n", "\r\n")
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.cfg.From, m.To, encodeSubject(m.Subject), body)
	var auth smtp.Auth
	if s.cfg.User != "" {
		auth = smtp.PlainAuth("", s.cfg.User, s.cfg.Pass.Reveal(), s.cfg.Host)
	}
	if !s.cfg.TLS {
		return smtp.SendMail(s.cfg.Addr(), auth, s.cfg.From, []string{m.To}, []byte(msg))
	}
	conn, err := net.Dial("tcp", s.cfg.Addr())
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(m.To); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	return w.Close()
}

func encodeSubject(subject string) string {
	if isASCII(subject) {
		return subject
	}
	return "=?UTF-8?B?" + b64(subject) + "?="
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func b64(s string) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	src := []byte(s)
	out := make([]byte, 0, (len(src)+2)/3*4)
	for len(src) > 0 {
		var b [3]byte
		n := copy(b[:], src)
		src = src[n:]
		out = append(out, table[b[0]>>2], table[((b[0]&0x3)<<4)|(b[1]>>4)])
		if n == 1 {
			out = append(out, '=', '=')
			break
		}
		out = append(out, table[((b[1]&0xf)<<2)|(b[2]>>6)])
		if n == 2 {
			out = append(out, '=')
			break
		}
		out = append(out, table[b[2]&0x3f])
	}
	return string(out)
}

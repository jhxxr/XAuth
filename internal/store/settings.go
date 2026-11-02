package store

import (
	"context"
	"database/sql"
	"errors"
)

// Registration modes, stored under SettingRegistrationMode.
const (
	RegistrationClosed = "closed"
	RegistrationInvite = "invite"
	RegistrationOpen   = "open"
)

// Setting keys whose values are plain text rather than random secrets.
const (
	SettingRegistrationMode  = "registration.mode"
	SettingInviteVerifyEmail = "registration.invite_verify_email"
)

// RegistrationSettings is the account-creation policy chosen in the admin UI.
type RegistrationSettings struct {
	Mode              string // RegistrationClosed, RegistrationInvite or RegistrationOpen
	InviteVerifyEmail bool   // invited users must confirm their email address
}

// ValidRegistrationMode reports whether mode is one of the known values.
func ValidRegistrationMode(mode string) bool {
	switch mode {
	case RegistrationClosed, RegistrationInvite, RegistrationOpen:
		return true
	}
	return false
}

// ReadSetting returns a text setting. ok is false when the key was never set.
func (s *Store) ReadSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	var v []byte
	err = s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(v), true, nil
}

// WriteSetting stores a text setting, replacing any previous value.
func (s *Store) WriteSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, []byte(value))
	return err
}

// Registration returns the stored policy, using def for values the admin
// never changed.
func (s *Store) Registration(ctx context.Context, def RegistrationSettings) (RegistrationSettings, error) {
	out := def
	v, ok, err := s.ReadSetting(ctx, SettingRegistrationMode)
	if err != nil {
		return out, err
	}
	if ok && ValidRegistrationMode(v) {
		out.Mode = v
	}
	v, ok, err = s.ReadSetting(ctx, SettingInviteVerifyEmail)
	if err != nil {
		return out, err
	}
	if ok {
		out.InviteVerifyEmail = v == "1"
	}
	return out, nil
}

// SetRegistration saves the policy, both fields at once.
func (s *Store) SetRegistration(ctx context.Context, r RegistrationSettings) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	verify := "0"
	if r.InviteVerifyEmail {
		verify = "1"
	}
	for key, value := range map[string]string{
		SettingRegistrationMode:  r.Mode,
		SettingInviteVerifyEmail: verify,
	} {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			key, []byte(value)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

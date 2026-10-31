// Package password hashes and checks user passwords with bcrypt.
package password

import (
	"errors"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Cost is the bcrypt work factor. Tests lower it to keep them fast.
var Cost = 12

// MinLength is the minimum password length in characters.
const MinLength = 8

var (
	ErrTooShort = errors.New("password is too short")
	// ErrTooLong is returned past bcrypt's 72-byte input limit.
	ErrTooLong = errors.New("password is longer than 72 bytes")
)

// Validate checks the length rules.
func Validate(pw string) error {
	if utf8.RuneCountInString(pw) < MinLength {
		return ErrTooShort
	}
	if len(pw) > 72 {
		return ErrTooLong
	}
	return nil
}

// Hash returns a bcrypt hash of pw.
func Hash(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), Cost)
	return string(b), err
}

// Check reports whether pw matches hash. An empty hash never matches but
// still costs one bcrypt comparison, so unknown accounts take as long to
// reject as known ones.
func Check(hash, pw string) bool {
	if hash == "" {
		bcrypt.CompareHashAndPassword([]byte(dummy()), []byte(pw))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

func dummy() string {
	dummyOnce.Do(func() {
		dummyHash, _ = Hash("xauth-timing-equaliser")
	})
	return dummyHash
}

package totp

import (
	"bytes"
	"encoding/base32"
	"errors"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B uses a different key length for each hash algorithm.
func TestRFC6238AllAlgorithms(t *testing.T) {
	secrets := map[string]string{
		SHA1:   "12345678901234567890",
		SHA256: "12345678901234567890123456789012",
		SHA512: "1234567890123456789012345678901234567890123456789012345678901234",
	}
	cases := []struct {
		unix  int64
		codes [3]string
	}{
		{59, [3]string{"94287082", "46119246", "90693936"}},
		{1111111109, [3]string{"07081804", "68084774", "25091201"}},
		{1111111111, [3]string{"14050471", "67062674", "99943326"}},
		{1234567890, [3]string{"89005924", "91819424", "93441116"}},
		{2000000000, [3]string{"69279037", "90698825", "38618901"}},
		{20000000000, [3]string{"65353130", "77737706", "47863826"}},
	}
	for _, tc := range cases {
		for i, algorithm := range []string{SHA1, SHA256, SHA512} {
			entry := Entry{Secret: base32.StdEncoding.EncodeToString([]byte(secrets[algorithm])), Digits: 8, Algorithm: algorithm}
			got, err := Code(entry, time.Unix(tc.unix, 0))
			if err != nil || got != tc.codes[i] {
				t.Errorf("%s at %d: got %q, %v; want %s", algorithm, tc.unix, got, err, tc.codes[i])
			}
		}
	}
}

func TestParsePreservesNonDefaultPeriod(t *testing.T) {
	for _, period := range []int{1, 10, 300, 86400} {
		got, err := Parse("JBSWY3DPEHPK3PXP", Entry{Period: period})
		if err != nil || got.Period != period {
			t.Errorf("period %d: got %d, %v", period, got.Period, err)
		}
	}
}

func TestParseRejectsInvalidParameters(t *testing.T) {
	for _, query := range []string{
		"digits=5", "digits=9", "digits=0", "digits=wrong", "digits=6.5",
		"period=0", "period=-30", "period=wrong", "period=30.5", "period=86401",
		"algorithm=MD5", "algorithm=SHA3", "encoder=steam", "secret=JBSWY3DPEHPK3PXP",
	} {
		t.Run(query, func(t *testing.T) {
			_, err := Parse("otpauth://totp/test?secret=JBSWY3DPEHPK3PXP&"+query, Entry{})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted invalid parameters: %v", err)
			}
		})
	}
	for _, overrides := range []Entry{{Digits: -1}, {Digits: 9}, {Period: -1}, {Period: 86401}, {Algorithm: "MD5"}} {
		if _, err := Parse("JBSWY3DPEHPK3PXP", overrides); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted invalid overrides %+v: %v", overrides, err)
		}
	}
}

func TestParseLabelUnescapesOnce(t *testing.T) {
	got, err := Parse("otpauth://totp/Example:alice%252Fbob?secret=JBSWY3DPEHPK3PXP", Entry{})
	if err != nil || got.AccountName != "alice%2Fbob" {
		t.Fatalf("got account %q, %v", got.AccountName, err)
	}
}

// RFC 6238 appendix B, 8-digit SHA1, secret ASCII "12345678901234567890".
func TestRFC6238SHA1(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	cases := []struct {
		unix int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	entry := Entry{Secret: secret, Digits: 8, Period: 30, Algorithm: SHA1}
	for _, tc := range cases {
		got, err := Code(entry, time.Unix(tc.unix, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.code {
			t.Errorf("at %d: got %s, want %s", tc.unix, got, tc.code)
		}
	}
}

func TestRFC6238SHA256AndSHA512(t *testing.T) {
	at := time.Unix(1111111109, 0)
	sha256, err := Code(Entry{Secret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA", Digits: 8, Algorithm: SHA256}, at)
	if err != nil || sha256 != "68084774" {
		t.Fatalf("sha256: %s %v", sha256, err)
	}
	sha512, err := Code(Entry{Secret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNA", Digits: 8, Algorithm: SHA512}, at)
	if err != nil || sha512 != "25091201" {
		t.Fatalf("sha512: %s %v", sha512, err)
	}
}

func TestParseOtpAuth(t *testing.T) {
	got, err := Parse("otpauth://totp/GitHub:ada?secret=gezd-gnbv gy3t&issuer=GitHub&digits=8&period=60&algorithm=SHA256", Entry{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret != "GEZDGNBVGY3T" || got.Issuer != "GitHub" || got.AccountName != "ada" {
		t.Fatalf("%+v", got)
	}
	if got.Digits != 8 || got.Period != 60 || got.Algorithm != SHA256 || got.Name != "GitHub (ada)" {
		t.Fatalf("%+v", got)
	}
}

func TestParseRejectsHotpAndGarbage(t *testing.T) {
	if _, err := Parse("otpauth://hotp/x?secret=GEZDGNBV", Entry{}); err == nil {
		t.Fatal("hotp accepted")
	}
	if _, err := Parse("not!a!secret", Entry{}); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := Encrypt(key, []byte("GEZDGNBVGY3TQOJQ"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("GEZD")) {
		t.Fatal("ciphertext contains the secret")
	}
	plain, err := Decrypt(key, sealed)
	if err != nil || string(plain) != "GEZDGNBVGY3TQOJQ" {
		t.Fatalf("%q %v", plain, err)
	}
	if _, err := Decrypt(bytes.Repeat([]byte{8}, 32), sealed); err == nil {
		t.Fatal("wrong key decrypted")
	}
}

func TestRemainingSeconds(t *testing.T) {
	if got := RemainingSeconds(30, time.Unix(59, 0)); got != 1 {
		t.Fatal(got)
	}
	if got := RemainingSeconds(30, time.Unix(60, 0)); got != 30 {
		t.Fatal(got)
	}
	if strings.TrimLeft("0001", "0") != "1" {
		t.Fatal("pad assumption")
	}
}

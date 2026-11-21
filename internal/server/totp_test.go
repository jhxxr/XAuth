package server_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTotpReplacingBareSecretPreservesParameters(t *testing.T) {
	e := newEnv(t)
	e.user("totp@example.com", "totp-password", true)
	c := e.browser()
	e.api(c, http.MethodPost, "/api/login", map[string]string{"email": "totp@example.com", "password": "totp-password"})
	e.st.SetClock(func() time.Time { return time.Unix(1111111109, 500000000) })
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQGEZA"
	for _, path := range []string{"/api/totp", "/api/admin/totp"} {
		t.Run(path, func(t *testing.T) {
			var created struct {
				Entry struct {
					ID string `json:"id"`
				} `json:"entry"`
			}
			r := e.api(c, http.MethodPost, path, map[string]any{
				"input": "otpauth://totp/Test:alice?secret=" + secret + "&digits=8&period=60&algorithm=SHA256",
			})
			if r.status != http.StatusCreated {
				t.Fatalf("create: %d %s", r.status, r.body)
			}
			r.json(t, &created)
			r = e.api(c, http.MethodPatch, path+"/"+created.Entry.ID, map[string]any{"input": secret})
			if r.status != http.StatusOK {
				t.Fatalf("update: %d %s", r.status, r.body)
			}
			var updated struct {
				Entry struct {
					Digits    int    `json:"digits"`
					Period    int    `json:"period"`
					Algorithm string `json:"algorithm"`
				} `json:"entry"`
			}
			r.json(t, &updated)
			if updated.Entry.Digits != 8 || updated.Entry.Period != 60 || updated.Entry.Algorithm != "SHA256" {
				t.Fatalf("replacing the secret reset its parameters: %s", r.body)
			}
		})
	}
}

func TestTotpRejectsInvalidParametersWithoutCreatingEntry(t *testing.T) {
	e := newEnv(t)
	e.user("totp@example.com", "totp-password", true)
	c := e.browser()
	e.api(c, http.MethodPost, "/api/login", map[string]string{"email": "totp@example.com", "password": "totp-password"})
	for _, path := range []string{"/api/totp", "/api/admin/totp"} {
		for _, fields := range []map[string]any{
			{"digits": 0}, {"period": 0}, {"digits": -1}, {"period": -30}, {"digits": 9}, {"period": 86401}, {"algorithm": "MD5"},
		} {
			fields["input"] = "JBSWY3DPEHPK3PXP"
			r := e.api(c, http.MethodPost, path, fields)
			if r.status != http.StatusBadRequest {
				t.Errorf("%s %+v: got %d %s", path, fields, r.status, r.body)
			}
		}
		for _, parameter := range []string{"digits=5", "period=0", "algorithm=MD5"} {
			r := e.api(c, http.MethodPost, path, map[string]any{
				"input":  "otpauth://totp/test?secret=JBSWY3DPEHPK3PXP&" + parameter,
				"digits": 6, "period": 30, "algorithm": "SHA1",
			})
			if r.status != http.StatusBadRequest {
				t.Errorf("%s %s: got %d %s", path, parameter, r.status, r.body)
			}
		}
		var list struct {
			Entries []any `json:"entries"`
		}
		e.get(c, path).json(t, &list)
		if len(list.Entries) != 0 {
			t.Errorf("%s stored invalid entries", path)
		}
	}
}

func TestTotpVault(t *testing.T) {
	e := newEnv(t)
	e.user("admin@example.com", "admin-pass", true)
	bob := e.user("bob@example.com", "bob-pass", false)
	e.user("cara@example.com", "cara-pass", false)

	admin := e.browser()
	e.api(admin, http.MethodPost, "/api/login", map[string]string{"email": "admin@example.com", "password": "admin-pass"})
	anon := e.browser()
	if r := e.api(anon, http.MethodGet, "/api/admin/totp", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("anonymous admin list: %d", r.status)
	}
	bobc := e.browser()
	e.api(bobc, http.MethodPost, "/api/login", map[string]string{"email": "bob@example.com", "password": "bob-pass"})
	if r := e.api(bobc, http.MethodGet, "/api/admin/totp", nil); r.status != http.StatusForbidden {
		t.Fatalf("user admin list: %d", r.status)
	}

	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	var created struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	r := e.api(admin, http.MethodPost, "/api/admin/totp", map[string]any{
		"input":   "otpauth://totp/GitHub:ops?secret=" + secret + "&issuer=GitHub&digits=8",
		"userIds": []string{bob.ID},
	})
	if r.status != http.StatusCreated {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	r.json(t, &created)
	if strings.Contains(string(r.body), secret) {
		t.Fatal("create response contains the secret")
	}

	var mine struct {
		Entries []struct {
			ID          string `json:"id"`
			Code        string `json:"code"`
			Remaining   int    `json:"remaining"`
			Shared      bool   `json:"shared"`
			GeneratedAt int64  `json:"generatedAt"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"entries"`
	}
	e.st.SetClock(func() time.Time { return time.Unix(59, 0) })
	r = e.get(bobc, "/api/totp")
	r.json(t, &mine)
	if len(mine.Entries) != 1 || mine.Entries[0].Code != "94287082" || !mine.Entries[0].Shared {
		t.Fatalf("bob codes: %s", r.body)
	}
	if mine.Entries[0].GeneratedAt != 59000 || mine.Entries[0].ExpiresAt != 60000 {
		t.Fatalf("missing server validity window: %s", r.body)
	}
	if strings.Contains(string(r.body), secret) {
		t.Fatal("code response contains the secret")
	}

	cara := e.browser()
	e.api(cara, http.MethodPost, "/api/login", map[string]string{"email": "cara@example.com", "password": "cara-pass"})
	r = e.get(cara, "/api/totp")
	r.json(t, &mine)
	if len(mine.Entries) != 0 {
		t.Fatalf("cara sees a shared entry: %s", r.body)
	}

	personal := "otpauth://totp/Bank:cara?secret=GEZDGNBVGY3TQOJQ&issuer=Bank"
	r = e.api(cara, http.MethodPost, "/api/totp", map[string]string{"input": personal})
	if r.status != http.StatusCreated {
		t.Fatalf("personal create: %d %s", r.status, r.body)
	}
	var own struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	r.json(t, &own)
	if r := e.api(admin, http.MethodPatch, "/api/admin/totp/"+own.Entry.ID, map[string]any{"name": "stolen"}); r.status != http.StatusNotFound {
		t.Fatalf("admin edited a personal entry: %d %s", r.status, r.body)
	}
	if r := e.get(bobc, "/api/totp"); strings.Contains(string(r.body), own.Entry.ID) {
		t.Fatal("bob sees cara's personal entry")
	}

	if r := e.api(admin, http.MethodPut, "/api/admin/totp/"+created.Entry.ID+"/grants", map[string]any{"userIds": []string{}}); r.status != http.StatusOK {
		t.Fatalf("unassign: %d %s", r.status, r.body)
	}
	r = e.get(bobc, "/api/totp")
	r.json(t, &mine)
	if len(mine.Entries) != 0 {
		t.Fatalf("grant survived: %s", r.body)
	}

	events := e.get(admin, "/api/admin/events?limit=20")
	if strings.Contains(string(events.body), secret) {
		t.Fatal("audit log contains the secret")
	}
}

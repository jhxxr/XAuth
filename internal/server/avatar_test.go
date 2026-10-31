package server_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

// pngBytes builds a small valid PNG so the server's content sniffing sees a
// real image.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func dataURL(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

type avatarUser struct {
	User struct {
		AvatarURL *string `json:"avatarUrl"`
	} `json:"user"`
}

func TestAvatarUploadAndServe(t *testing.T) {
	e := newEnv(t)
	u := e.user("pia@example.com", "correct horse", false)
	c := e.browser()
	login(e, c, u.Email, "correct horse")

	img := pngBytes(t)
	var out avatarUser
	r := e.api(c, http.MethodPut, "/api/account/avatar", map[string]string{"image": dataURL("image/png", img)})
	if r.status != http.StatusOK {
		t.Fatalf("upload: %d %s", r.status, r.body)
	}
	r.json(t, &out)
	if out.User.AvatarURL == nil || !strings.HasPrefix(*out.User.AvatarURL, "/api/users/") {
		t.Fatalf("avatar url: %+v", out.User.AvatarURL)
	}
	url := *out.User.AvatarURL

	// The session says the same thing, so a reload keeps showing the picture.
	var sess avatarUser
	e.api(c, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User.AvatarURL == nil || *sess.User.AvatarURL != url {
		t.Fatalf("session avatar url: %+v", sess.User.AvatarURL)
	}

	// Admins see the same URL in the user list.
	u.IsAdmin = true
	if err := e.st.UpdateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Users []struct {
			Email     string  `json:"email"`
			AvatarURL *string `json:"avatarUrl"`
		} `json:"users"`
	}
	r = e.api(c, http.MethodGet, "/api/admin/users", nil)
	if r.status != http.StatusOK {
		t.Fatalf("list users: %d %s", r.status, r.body)
	}
	r.json(t, &listed)
	if len(listed.Users) != 1 || listed.Users[0].AvatarURL == nil || *listed.Users[0].AvatarURL != url {
		t.Fatalf("listed avatar url: %+v", listed.Users)
	}

	r = e.get(c, url)
	if r.status != http.StatusOK || r.header.Get("Content-Type") != "image/png" {
		t.Fatalf("fetch avatar: %d %q", r.status, r.header.Get("Content-Type"))
	}
	if !bytes.Equal(r.body, img) {
		t.Fatal("served bytes differ from the upload")
	}
	etag := r.header.Get("ETag")
	if etag == "" {
		t.Fatal("no etag on avatar")
	}
	if r := do(t, c, http.MethodGet, e.abs(url), nil, map[string]string{"If-None-Match": etag}); r.status != http.StatusNotModified {
		t.Fatalf("conditional get: %d", r.status)
	}

	// Signed-out visitors cannot read pictures.
	if r := e.get(http.DefaultClient, url); r.status != http.StatusUnauthorized {
		t.Fatalf("anonymous avatar fetch: %d", r.status)
	}

	// Removing the picture clears both the bytes and the URL.
	r = e.api(c, http.MethodDelete, "/api/account/avatar", nil)
	if r.status != http.StatusOK {
		t.Fatalf("delete avatar: %d %s", r.status, r.body)
	}
	r.json(t, &out)
	if out.User.AvatarURL != nil {
		t.Fatalf("avatar url after delete: %+v", out.User.AvatarURL)
	}
	if r := e.get(c, url); r.status != http.StatusNotFound {
		t.Fatalf("fetch deleted avatar: %d", r.status)
	}
}

func TestAvatarRejectsBadInput(t *testing.T) {
	e := newEnv(t)
	u := e.user("quill@example.com", "correct horse", false)
	c := e.browser()
	login(e, c, u.Email, "correct horse")

	cases := []struct{ name, image, wantCode string }{
		{"empty", "", "invalid_avatar"},
		{"not base64", "hello there", "invalid_avatar"},
		{"html", base64.StdEncoding.EncodeToString([]byte("<html><script>alert(1)</script></html>")), "invalid_avatar_type"},
		{"oversized", dataURL("image/png", make([]byte, 2<<20)), "avatar_too_large"},
	}
	for _, tc := range cases {
		r := e.api(c, http.MethodPut, "/api/account/avatar", map[string]string{"image": tc.image})
		if r.status != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", tc.name, r.status, r.body)
		}
		var apiErr struct{ Error string }
		r.json(t, &apiErr)
		if apiErr.Error != tc.wantCode {
			t.Fatalf("%s: got %q, want %q", tc.name, apiErr.Error, tc.wantCode)
		}
	}

	// A body far beyond the JSON limit is a 413, not a 400.
	r := e.api(c, http.MethodPut, "/api/account/avatar", map[string]string{"image": strings.Repeat("A", 4<<20)})
	if r.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("huge body: %d %s", r.status, r.body)
	}

	// A failed upload must not have replaced anything.
	var sess avatarUser
	e.api(c, http.MethodGet, "/api/session", nil).json(t, &sess)
	if sess.User.AvatarURL != nil {
		t.Fatalf("avatar url after rejected uploads: %+v", sess.User.AvatarURL)
	}
}

func TestAvatarRequiresSignIn(t *testing.T) {
	e := newEnv(t)
	if r := e.api(http.DefaultClient, http.MethodPut, "/api/account/avatar", map[string]string{"image": ""}); r.status != http.StatusUnauthorized {
		t.Fatalf("anonymous upload: %d %s", r.status, r.body)
	}
}

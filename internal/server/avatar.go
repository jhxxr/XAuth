package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xauth/internal/store"
)

// avatarMIME lists the image types XAuth stores and serves. Anything else
// (SVG in particular) is refused: it can carry script.
var avatarMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// avatarBodyLimit bounds the JSON request. Base64 expands a picture by 4/3
// and the client may send a data: URL, so allow some headroom.
const avatarBodyLimit = 3 << 20

// avatarURL is the cache-busting URL of a user's picture, or "" when the
// user has none.
func avatarURL(u *store.User) string {
	if u.AvatarUpdatedAt.IsZero() {
		return ""
	}
	return "/api/users/" + url.PathEscape(u.ID) + "/avatar?v=" + strconv.FormatInt(u.AvatarUpdatedAt.Unix(), 10)
}

// parseAvatarImage accepts either a bare base64 string or a data: URL and
// returns the sniffed MIME type. The client's own content type is ignored:
// the bytes decide what the file is.
func parseAvatarImage(raw string) (mime string, data []byte, code string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, "invalid_avatar"
	}
	if rest, ok := strings.CutPrefix(raw, "data:"); ok {
		comma := strings.IndexByte(rest, ',')
		if comma < 0 || !strings.HasSuffix(rest[:comma], ";base64") {
			return "", nil, "invalid_avatar"
		}
		raw = strings.TrimSpace(rest[comma+1:])
	}
	// Reject oversized input before allocating the decode buffer.
	if len(raw) > base64.StdEncoding.EncodedLen(store.MaxAvatarBytes) {
		return "", nil, "avatar_too_large"
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 {
		return "", nil, "invalid_avatar"
	}
	if len(decoded) > store.MaxAvatarBytes {
		return "", nil, "avatar_too_large"
	}
	mime = http.DetectContentType(decoded)
	if !avatarMIME[mime] {
		return "", nil, "invalid_avatar_type"
	}
	return mime, decoded, ""
}

func (s *Server) handleSetAvatar(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Image string `json:"image"`
	}
	if !decodeLimit(w, r, avatarBodyLimit, &req) {
		return
	}
	mime, data, code := parseAvatarImage(req.Image)
	if code != "" {
		writeError(w, http.StatusBadRequest, code)
		return
	}
	if _, err := s.st.SetAvatar(r.Context(), info.user.ID, mime, data); err != nil {
		s.internalError(w, r, err)
		return
	}
	u, err := s.st.UserByID(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvAvatarSet, ActorID: u.ID, IP: s.clientIP(r),
		Detail: map[string]any{"type": mime, "bytes": len(data)}})
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}

func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	info, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if _, err := s.st.DeleteAvatar(r.Context(), info.user.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	u, err := s.st.UserByID(r.Context(), info.user.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r.Context(), store.Event{Type: store.EvAvatarRemove, ActorID: u.ID, IP: s.clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}

// handleAvatar serves a profile picture. Only signed-in people can fetch one,
// so the pictures of a deployment stay private.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if sessionFrom(r.Context()) == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	av, err := s.st.Avatar(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	etag := `"` + strconv.FormatInt(av.UpdatedAt.Unix(), 10) + `"`
	h := w.Header()
	// The /api group sets no-store; pictures are immutable per version, so a
	// private cache is fine and saves a request per page view.
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", av.MIME)
	h.Set("Content-Length", strconv.Itoa(len(av.Data)))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(av.Data)
}

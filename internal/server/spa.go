package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaCSP applies to the web UI pages only. The library's form_post response
// needs an inline script, so provider responses do not get this policy.
const spaCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"

// serveSPA serves built assets and falls back to index.html for app routes.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if s.ui == nil {
		noUI(w)
		return
	}
	p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if p != "" && p != "index.html" && !strings.HasPrefix(path.Base(p), ".") {
		if info, err := fs.Stat(s.ui, p); err == nil && !info.IsDir() {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFileFS(w, r, s.ui, p)
			return
		}
		if path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
	}
	index, err := fs.ReadFile(s.ui, "index.html")
	if err != nil {
		noUI(w)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Security-Policy", spaCSP)
	_, _ = w.Write(index)
}

func noUI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("XAuth web UI is not built. Run: cd web && npm install && npm run build, then rebuild the server.\n"))
}

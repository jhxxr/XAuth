package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter counts events per key in a fixed window. It is in-memory, so the
// counts reset when XAuth restarts.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
}

type bucket struct {
	count int
	reset time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string]*bucket{}}
}

// allowed reports whether key is still under the limit.
func (l *limiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.hits[key]
	return b == nil || time.Now().After(b.reset) || b.count < l.max
}

// hit records one event for key.
func (l *limiter) hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 10000 {
		for k, b := range l.hits {
			if now.After(b.reset) {
				delete(l.hits, k)
			}
		}
	}
	b := l.hits[key]
	if b == nil || now.After(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.count++
}

func (l *limiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, key)
}

// clientIP returns the caller's address. Proxy headers are only trusted with
// XAUTH_TRUST_PROXY; the last X-Forwarded-For entry is the one the proxy added.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			parts := strings.Split(v, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

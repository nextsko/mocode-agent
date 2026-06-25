package httputil

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// RequestLogging wraps h with structured request logging.
func RequestLogging(entrypoint string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rw, r)
		slog.Debug("http request",
			"entrypoint", entrypoint,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// AllowLocalOrigin returns true for localhost and loopback WebSocket origins.
func AllowLocalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u := strings.ToLower(origin)
	return strings.Contains(u, "localhost") ||
		strings.Contains(u, "127.0.0.1") ||
		strings.Contains(u, "[::1]")
}

// IsLoopbackRequest reports whether the client address is loopback.
func IsLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// BearerAuth requires Authorization: Bearer <token> when token is non-empty.
func BearerAuth(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) || strings.TrimPrefix(auth, prefix) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

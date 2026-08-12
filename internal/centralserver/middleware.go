package centralserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

type requestIDKey struct{}

// withMiddleware wraps mux with the server's cross-cutting HTTP concerns,
// outermost first: a request ID (correlates every log line for one
// request), panic recovery (a handler bug becomes a 500, not a crashed
// process), an access log, and request metrics.
func (s *Server) withMiddleware(mux *http.ServeMux) http.Handler {
	return s.withRequestID(s.withRecover(s.withAccessLog(mux)))
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.ErrorContext(r.Context(), "panic handling request",
					"method", r.Method, "path", r.URL.Path, "request_id", requestID(r.Context()), "panic", rec)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withAccessLog logs and records metrics for every request. Metrics use the
// matched route *pattern* ("/api/v1/recipes/{name}"), not the raw path —
// the raw path contains arbitrary Recipe/source names, and Prometheus label
// values must stay low-cardinality or the series count grows unbounded.
// mux.Handler is a pure lookup (no side effects), safe to call before
// dispatch.
func (s *Server) withAccessLog(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		if pattern == "" {
			pattern = "unmatched"
		}

		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		mux.ServeHTTP(sw, r)

		duration := time.Since(start)
		s.log.InfoContext(r.Context(), "request",
			"method", r.Method,
			"path", r.URL.Path,
			"route", pattern,
			"status", sw.status,
			"duration_ms", duration.Milliseconds(),
			"request_id", requestID(r.Context()),
		)
		s.metrics.observe(r.Method, pattern, sw.status, duration)
	})
}

// statusWriter captures the status code a handler wrote, since
// http.ResponseWriter doesn't expose it after the fact.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// requestID returns the current request's correlation ID, or "" outside an
// HTTP request (e.g. a direct call in a test).
func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

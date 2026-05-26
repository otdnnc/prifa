// Package logx wires the project to log/slog. It exposes:
//
//   - New: build a configured Logger.
//   - WithLogger/FromContext: pass a request-scoped logger through context.
//   - AccessLog: middleware that tags requests with a request id and emits
//     one structured access log line per request.
package logx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"prifa/internal/room"
)

// Format selects the output encoding.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// New builds a Logger writing to w. Pass os.Stdout for the default.
func New(w io.Writer, level slog.Level, format Format) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	switch format {
	case FormatJSON:
		h = slog.NewJSONHandler(w, opts)
	default:
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}

// ParseLevel maps strings like "debug" / "info" to slog.Level. Unknown
// values fall back to info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type ctxKey struct{}

// WithLogger attaches a request-scoped logger to ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the request-scoped logger, falling back to the
// process default if none is attached.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// RequestIDHeader is the canonical request id header on both sides.
const RequestIDHeader = "X-Request-Id"

// AccessLog wraps next with request-id propagation and access logging.
// One log line is emitted per request when its handler returns; for
// long-lived streams (SSE, media subscribe/publish) the line captures
// total bytes and stream duration, which is useful operationally.
func AccessLog(base *slog.Logger) func(http.Handler) http.Handler {
	if base == nil {
		base = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := r.Header.Get(RequestIDHeader)
			if reqID == "" {
				reqID = room.NewID(6)
			}
			w.Header().Set(RequestIDHeader, reqID)

			l := base.With(
				"request_id", reqID,
				"method", r.Method,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"proto", r.Proto,
			)
			rw := newStatusWriter(w)
			start := time.Now()
			defer func() {
				attrs := []any{
					"status", rw.status,
					"bytes", rw.bytes,
					"duration_ms", time.Since(start).Milliseconds(),
				}
				if rw.status >= 500 {
					l.Error("request", attrs...)
				} else if rw.status >= 400 {
					l.Warn("request", attrs...)
				} else {
					l.Info("request", attrs...)
				}
			}()
			next.ServeHTTP(rw, r.WithContext(WithLogger(r.Context(), l)))
		})
	}
}

// statusWriter records the response status and total bytes written, while
// transparently forwarding Flush() so streaming endpoints stay live.
type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (s *statusWriter) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.wroteHeader = true
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// Flush surfaces the underlying flusher so SSE / streaming media work
// under the wrapper. If the underlying writer doesn't implement Flusher
// (rare), this is a no-op.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets net/http's ResponseController find the real writer.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// IsClientGone reports whether an error indicates the peer disconnected
// (as opposed to a real server fault). Useful for streaming endpoints
// that should log such cases at debug rather than error level.
func IsClientGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

// Package observability provides the API's structured logger and the HTTP
// middleware that ties each request to a request-scoped logger.
package observability

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const loggerKey ctxKey = iota

// NewLogger builds the process logger. JSON in every environment so logs are
// machine-parseable; level is Debug in development, Info otherwise.
func NewLogger(appEnv string) *slog.Logger {
	level := slog.LevelInfo
	if appEnv == "development" {
		level = slog.LevelDebug
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(handler)
}

// WithLogger returns a context carrying logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFrom returns the request-scoped logger, or the default logger when none
// was attached (e.g. in a unit test that does not run the middleware).
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// statusRecorder captures the response status for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// RequestLogger attaches a request-scoped logger (tagged with the chi request
// ID) to the context and emits one structured access log per request. It must
// run after chi's middleware.RequestID so the ID is present.
func RequestLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := middleware.GetReqID(r.Context())
			reqLogger := base.With(slog.String("request_id", reqID))

			ctx := WithLogger(r.Context(), reqLogger)
			rec := &statusRecorder{ResponseWriter: w}
			start := time.Now()

			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			reqLogger.LogAttrs(r.Context(), levelForStatus(rec.status), "http_request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote_ip", r.RemoteAddr),
			)
		})
	}
}

func levelForStatus(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

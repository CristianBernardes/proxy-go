package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// Logging registra método, path e duração de cada requisição.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("http", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
		})
	}
}

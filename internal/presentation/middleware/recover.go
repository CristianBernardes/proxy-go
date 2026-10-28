package middleware

import (
	"log/slog"
	"net/http"

	"edenred-ai-proxy/internal/presentation/httpx"
)

// Recover captura panics em qualquer handler, evitando que o processo derrube o servidor.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("http", "panic", rec)
					httpx.Error(w, http.StatusInternalServerError, "Internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

package middleware

import "net/http"

// Middleware decora um http.Handler com comportamento adicional (Decorator pattern).
type Middleware func(http.Handler) http.Handler

// Chain aplica os middlewares na ordem informada — o primeiro é o mais externo.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

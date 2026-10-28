package handler

import (
	"net/http"
	"time"

	"edenred-ai-proxy/internal/presentation/httpx"
)

// Health responde ao liveness check.
type Health struct{}

// NewHealth constrói o handler (stateless).
func NewHealth() *Health { return &Health{} }

// Handle atende GET /health, retornando status 200 com timestamp.
func (h *Health) Handle(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"ts":     time.Now().Format(time.RFC3339),
	})
}

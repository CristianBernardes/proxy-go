package handler

import (
	"net/http"

	"edenred-ai-proxy/internal/application/usecase"
	"edenred-ai-proxy/internal/presentation/httpx"
)

// Config expõe o JSON de "chat.models" para o VS Code via /vscode-config.
type Config struct {
	generateConfig *usecase.GenerateVSCodeConfig
}

// NewConfig constrói o handler com o caso de uso injetado.
func NewConfig(generateConfig *usecase.GenerateVSCodeConfig) *Config {
	return &Config{generateConfig: generateConfig}
}

// Handle atende GET /vscode-config, retornando o JSON de "chat.models" pronto
// para copiar ao settings.json do VS Code.
func (h *Config) Handle(w http.ResponseWriter, r *http.Request) {
	result, err := h.generateConfig.Execute(r.Context(), usecase.GenerateOptions{Print: false})
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

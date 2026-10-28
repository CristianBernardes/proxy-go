package presentation

import (
	"log/slog"
	"net/http"

	"edenred-ai-proxy/internal/application/usecase"
	"edenred-ai-proxy/internal/infrastructure/config"
	"edenred-ai-proxy/internal/presentation/handler"
	"edenred-ai-proxy/internal/presentation/middleware"
)

// NewRouter monta o multiplexador HTTP com todas as rotas do proxy.
func NewRouter(
	getModels *usecase.GetModels,
	chatCompletion *usecase.ChatCompletion,
	ensemble *usecase.Ensemble,
	generateConfig *usecase.GenerateVSCodeConfig,
	cfg *config.EnvConfig,
	logger *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	health := handler.NewHealth()
	openai := handler.NewOpenAI(getModels, chatCompletion, ensemble, cfg, logger)
	ollama := handler.NewOllama(getModels, chatCompletion, cfg, logger)
	cfgHandler := handler.NewConfig(generateConfig)

	mux.HandleFunc("GET /health", health.Handle)
	mux.HandleFunc("GET /v1/models", openai.ListModels)
	mux.HandleFunc("POST /v1/chat/completions", openai.ChatCompletions)
	mux.HandleFunc("GET /api/tags", ollama.ListTags)
	mux.HandleFunc("POST /api/chat", ollama.Chat)
	mux.HandleFunc("POST /api/generate", ollama.Generate)
	mux.HandleFunc("GET /api/ps", ollama.Ps)
	mux.HandleFunc("GET /vscode-config", cfgHandler.Handle)

	return middleware.Chain(mux, middleware.Recover(logger), middleware.Logging(logger))
}

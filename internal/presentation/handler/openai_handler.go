package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"edenred-ai-proxy/internal/application/usecase"
	"edenred-ai-proxy/internal/infrastructure/config"
	"edenred-ai-proxy/internal/presentation/httpx"
	"edenred-ai-proxy/internal/shared/streaming"
)

// OpenAI expõe as rotas compatíveis com a API OpenAI (/v1/*), usadas pelo VS Code Copilot.
type OpenAI struct {
	getModels      *usecase.GetModels
	chatCompletion *usecase.ChatCompletion
	ensemble       *usecase.Ensemble
	config         *config.EnvConfig
	logger         *slog.Logger
}

// NewOpenAI constrói o handler com suas dependências injetadas.
func NewOpenAI(getModels *usecase.GetModels, chatCompletion *usecase.ChatCompletion, ensemble *usecase.Ensemble, cfg *config.EnvConfig, logger *slog.Logger) *OpenAI {
	return &OpenAI{getModels: getModels, chatCompletion: chatCompletion, ensemble: ensemble, config: cfg, logger: logger}
}

// ListModels atende GET /v1/models, retornando os modelos no formato OpenAI.
func (h *OpenAI) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.getModels.Execute(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, m.ToOpenAIFormat())
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

// ChatCompletions atende POST /v1/chat/completions, encaminhando ao upstream
// e retornando stream SSE ou JSON conforme o parâmetro "stream" do payload.
// Se o modelo solicitado for o modelo virtual de consenso, delega ao Ensemble.
func (h *OpenAI) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	body := httpx.ParseBody(r)

	// Headers e pings saem antes de o upstream responder (ver StreamAsync).
	if stream, _ := body["stream"].(bool); stream {
		produce := func(ctx context.Context) (io.ReadCloser, error) {
			result, err := h.execute(ctx, body)
			if err != nil {
				return nil, err
			}
			return result.Body, nil
		}
		if err := streaming.StreamAsync(w, r, "text/event-stream", h.config.KeepAliveInterval, h.config.StreamBufferSize, produce, streaming.SSEError); err != nil {
			h.logger.Warn("stream interrompido", "err", err)
		}
		return
	}

	result, err := h.execute(r.Context(), body)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	defer result.Body.Close()

	httpx.CopyJSON(w, result.Status, result.Body)
}

// execute escolhe entre o fluxo normal e a Orquestração de Consenso (MoA).
func (h *OpenAI) execute(ctx context.Context, body map[string]any) (*usecase.ChatCompletionResult, error) {
	if h.config.EnsembleEnabled && body["tools"] == nil {
		model, _ := body["model"].(string)
		if h.config.EnsembleAlways || model == h.config.EnsembleModel {
			return h.ensemble.Execute(ctx, body)
		}
	}
	return h.chatCompletion.Execute(ctx, body)
}

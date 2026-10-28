package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"edenred-ai-proxy/internal/application/usecase"
	"edenred-ai-proxy/internal/infrastructure/config"
	"edenred-ai-proxy/internal/presentation/httpx"
	"edenred-ai-proxy/internal/shared/streaming"
)

// Ollama expõe as rotas compatíveis com a API Ollama (/api/*), usadas pelo Continue.
type Ollama struct {
	getModels      *usecase.GetModels
	chatCompletion *usecase.ChatCompletion
	config         *config.EnvConfig
	logger         *slog.Logger
}

// NewOllama constrói o handler com suas dependências injetadas.
func NewOllama(getModels *usecase.GetModels, chatCompletion *usecase.ChatCompletion, cfg *config.EnvConfig, logger *slog.Logger) *Ollama {
	return &Ollama{getModels: getModels, chatCompletion: chatCompletion, config: cfg, logger: logger}
}

// ListTags atende GET /api/tags, retornando os modelos no formato Ollama.
func (h *Ollama) ListTags(w http.ResponseWriter, r *http.Request) {
	models, err := h.getModels.Execute(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, m.ToOllamaFormat())
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"models": data})
}

// Chat atende POST /api/chat, convertendo o payload Ollama → OpenAI antes
// de encaminhar ao upstream, e reconvertendo a resposta OpenAI → Ollama.
func (h *Ollama) Chat(w http.ResponseWriter, r *http.Request) {
	body := httpx.ParseBody(r)

	model, _ := body["model"].(string)
	stream, _ := body["stream"].(bool)
	options, _ := body["options"].(map[string]any)

	// Converte formato Ollama → OpenAI para encaminhar ao upstream.
	openAIBody := map[string]any{
		"model":    model,
		"messages": body["messages"],
		"stream":   stream,
	}
	if options != nil {
		if v, ok := options["temperature"]; ok {
			openAIBody["temperature"] = v
		}
		if v, ok := options["top_p"]; ok {
			openAIBody["top_p"] = v
		}
		if v, ok := options["num_predict"]; ok {
			openAIBody["max_tokens"] = v
		}
	}

	if stream {
		h.streamNDJSON(w, r, openAIBody, model, false)
		return
	}

	result, err := h.chatCompletion.Execute(r.Context(), openAIBody)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	defer result.Body.Close()

	// Converte resposta OpenAI → Ollama.
	data, err := io.ReadAll(result.Body)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(data, &parsed)

	role, content := "assistant", ""
	if len(parsed.Choices) > 0 {
		role = parsed.Choices[0].Message.Role
		content = parsed.Choices[0].Message.Content
	}

	httpx.JSON(w, result.Status, map[string]any{
		"model":             model,
		"created_at":        time.Now().Format(time.RFC3339),
		"message":           map[string]any{"role": role, "content": content},
		"done":              true,
		"eval_count":        parsed.Usage.CompletionTokens,
		"prompt_eval_count": parsed.Usage.PromptTokens,
	})
}

// Generate atende POST /api/generate, usado por clientes Ollama que não
// falam o protocolo de chat (ex.: `ollama run`). Sem esta rota o mux
// retornava 404 antes de chegar ao upstream, o que explica respostas
// instantâneas (duration_ms=0) mesmo com o proxy no ar.
func (h *Ollama) Generate(w http.ResponseWriter, r *http.Request) {
	body := httpx.ParseBody(r)

	model, _ := body["model"].(string)
	prompt, _ := body["prompt"].(string)
	system, _ := body["system"].(string)
	stream, _ := body["stream"].(bool)
	options, _ := body["options"].(map[string]any)

	// /api/generate usa prompt único em vez de histórico de mensagens;
	// remontamos como messages para reaproveitar o mesmo caminho de chat.
	messages := make([]map[string]any, 0, 2)
	if system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	messages = append(messages, map[string]any{"role": "user", "content": prompt})

	openAIBody := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}
	if options != nil {
		if v, ok := options["temperature"]; ok {
			openAIBody["temperature"] = v
		}
		if v, ok := options["top_p"]; ok {
			openAIBody["top_p"] = v
		}
		if v, ok := options["num_predict"]; ok {
			openAIBody["max_tokens"] = v
		}
	}

	if stream {
		h.streamNDJSON(w, r, openAIBody, model, true)
		return
	}

	result, err := h.chatCompletion.Execute(r.Context(), openAIBody)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(data, &parsed)

	response := ""
	if len(parsed.Choices) > 0 {
		response = parsed.Choices[0].Message.Content
	}

	httpx.JSON(w, result.Status, map[string]any{
		"model":             model,
		"created_at":        time.Now().Format(time.RFC3339),
		"response":          response,
		"done":              true,
		"eval_count":        parsed.Usage.CompletionTokens,
		"prompt_eval_count": parsed.Usage.PromptTokens,
	})
}

// Ps atende GET /api/ps, usado por clientes Ollama para listar modelos
// carregados em memória. Como só repassamos ao upstream, não expomos esse
// conceito, então retornamos a lista vazia no formato esperado só para não
// quebrar clientes que fazem polling nesse endpoint antes de cada geração.
func (h *Ollama) Ps(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"models": []any{}})
}

// streamNDJSON executa o chat em stream e entrega NDJSON no formato Ollama
// (convertido do SSE OpenAI), sem pings.
func (h *Ollama) streamNDJSON(w http.ResponseWriter, r *http.Request, body map[string]any, model string, generate bool) {
	produce := func(ctx context.Context) (io.ReadCloser, error) {
		result, err := h.chatCompletion.Execute(ctx, body)
		if err != nil {
			return nil, err
		}
		return sseToOllamaNDJSON(result.Body, model, generate), nil
	}
	if err := streaming.StreamAsync(w, r, "application/x-ndjson", 0, h.config.StreamBufferSize, produce, streaming.NDJSONError); err != nil {
		h.logger.Warn("stream interrompido", "err", err)
	}
}

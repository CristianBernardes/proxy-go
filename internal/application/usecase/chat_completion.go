package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"edenred-ai-proxy/internal/domain/port"
	"edenred-ai-proxy/internal/infrastructure/config"
)

// ChatCompletionResult representa a resposta upstream sempre como stream de
// bytes — streaming e não-streaming compartilham o mesmo tipo (io.ReadCloser).
type ChatCompletionResult struct {
	Status   int
	Body     io.ReadCloser
	IsStream bool
}

// ChatCompletion sanitiza o payload e encaminha ao Ollama.
type ChatCompletion struct {
	config *config.EnvConfig
	client port.HTTPClient
	logger *slog.Logger
}

func NewChatCompletion(cfg *config.EnvConfig, client port.HTTPClient, logger *slog.Logger) *ChatCompletion {
	return &ChatCompletion{config: cfg, client: client, logger: logger}
}

// Execute sanitiza o payload (remove combinações incompatíveis como
// temperature+top_p) e encaminha ao Ollama, retornando sempre um io.ReadCloser
// independente de ser stream ou não.
func (uc *ChatCompletion) Execute(ctx context.Context, body map[string]any) (*ChatCompletionResult, error) {
	stream, _ := body["stream"].(bool)
	uc.logger.Info("chat_completion", "model", body["model"], "stream", stream)

	sanitized := sanitize(body)

	// Com "tools", buscamos a resposta completa (non-stream) para normalizar
	// tool calls que alguns modelos (ex.: Qwen) emitem como texto em vez do
	// formato estruturado esperado pelo cliente (modo agente do Copilot).
	if sanitized["tools"] != nil {
		res, handled, err := uc.executeWithTools(ctx, sanitized, stream)
		if err != nil {
			return nil, err
		}
		if handled {
			return res, nil
		}
		delete(sanitized, "tools")
		delete(sanitized, "tool_choice")
	}

	resp, err := uc.send(ctx, sanitized)
	if err != nil {
		return nil, err
	}

	if resp.Status >= 400 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		uc.logger.Error("chat_completion", "upstream_status", resp.Status, "body", truncate(data, 200))
		return nil, fmt.Errorf("upstream erro %d: %s", resp.Status, truncate(data, 200))
	}

	if stream {
		return &ChatCompletionResult{Status: resp.Status, Body: guardEmptyStream(resp.Body, sanitized["model"], uc.logger), IsStream: true}, nil
	}
	return &ChatCompletionResult{Status: resp.Status, Body: resp.Body, IsStream: false}, nil
}

// executeWithTools chama o Ollama em modo non-stream (necessário para inspecionar
// o corpo inteiro), normaliza eventuais tool calls emitidas como texto e devolve
// a resposta no formato pedido pelo cliente (SSE se ele pediu stream).
//
// handled=false sinaliza que o modelo não suporta tools — o chamador refaz a
// requisição sem ferramentas.
func (uc *ChatCompletion) executeWithTools(ctx context.Context, payload map[string]any, clientWantsStream bool) (*ChatCompletionResult, bool, error) {
	up := make(map[string]any, len(payload))
	for k, v := range payload {
		up[k] = v
	}
	up["stream"] = false

	resp, err := uc.send(ctx, up)
	if err != nil {
		return nil, false, err
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.Status == 400 && bytes.Contains(bytes.ToLower(data), []byte("does not support tools")) {
		uc.logger.Warn("chat_completion", "model", up["model"], "aviso", "modelo não suporta tools; refazendo sem tools")
		return nil, false, nil
	}
	if resp.Status >= 400 {
		uc.logger.Error("chat_completion", "upstream_status", resp.Status, "body", truncate(data, 200))
		return nil, false, fmt.Errorf("upstream erro %d: %s", resp.Status, truncate(data, 200))
	}

	normalized := ensureContent(normalizeToolCalls(data, toolNames(payload)), uc.logger)

	if clientWantsStream {
		sse := chatCompletionToSSE(normalized)
		return &ChatCompletionResult{Status: 200, Body: io.NopCloser(bytes.NewReader(sse)), IsStream: true}, true, nil
	}
	return &ChatCompletionResult{Status: 200, Body: io.NopCloser(bytes.NewReader(normalized)), IsStream: false}, true, nil
}

// send serializa o payload e faz a requisição ao endpoint de chat do Ollama.
func (uc *ChatCompletion) send(ctx context.Context, payload map[string]any) (*port.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return uc.client.Do(ctx, port.Request{
		Method: "POST",
		URL:    uc.config.OllamaURL + "/v1/chat/completions",
		Body:   data,
	})
}

// sanitize remove combinações incompatíveis — ex.: Databricks/Claude não
// aceita temperature e top_p simultaneamente.
func sanitize(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = v
	}
	if out["temperature"] != nil && out["top_p"] != nil {
		delete(out, "top_p")
	}
	return out
}

// truncate limita o tamanho de data a n bytes para evitar logs gigantes.
func truncate(data []byte, n int) string {
	if len(data) <= n {
		return string(data)
	}
	return string(data[:n])
}

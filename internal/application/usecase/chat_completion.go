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

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	normalized := ensureContent(data, uc.logger)
	return &ChatCompletionResult{Status: resp.Status, Body: io.NopCloser(bytes.NewReader(normalized)), IsStream: false}, nil
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

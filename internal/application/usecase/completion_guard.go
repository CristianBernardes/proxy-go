package usecase

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

var reasoningKeys = []string{"reasoning", "reasoning_content", "thinking"}

// ensureContent garante que toda choice de uma resposta non-stream tenha
// conteúdo utilizável (texto ou tool_calls) e remove as chaves de reasoning.
// Resposta sem nenhum dos dois faz o VS Code exibir "No response was returned".
// Também loga o resumo da resposta.
func ensureContent(data []byte, logger *slog.Logger) []byte {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return data
	}
	choices, ok := root["choices"].([]any)
	if !ok {
		return data
	}

	changed := false
	for _, c := range choices {
		ch, ok := c.(map[string]any)
		if !ok {
			continue
		}
		msg, ok := ch["message"].(map[string]any)
		if !ok {
			continue
		}

		content, _ := msg["content"].(string)
		toolCalls, _ := msg["tool_calls"].([]any)
		reasoning := ""
		for _, k := range reasoningKeys {
			if s, _ := msg[k].(string); strings.TrimSpace(s) != "" && reasoning == "" {
				reasoning = s
			}
			if _, has := msg[k]; has {
				delete(msg, k)
				changed = true
			}
		}

		finish, _ := ch["finish_reason"].(string)
		logger.Info("chat_completion_resposta",
			"model", root["model"], "finish_reason", finish,
			"content_len", len(content), "tool_calls", len(toolCalls),
			"reasoning_len", len(reasoning), "prompt_tokens", usageValue(root, "prompt_tokens"),
			"completion_tokens", usageValue(root, "completion_tokens"))

		if strings.TrimSpace(content) != "" || len(toolCalls) > 0 {
			continue
		}
		if reasoning != "" {
			msg["content"] = reasoning
		} else {
			msg["content"] = fmt.Sprintf("⚠ O modelo não retornou conteúdo (finish_reason=%s).", finish)
		}
		changed = true
	}

	if !changed {
		return data
	}
	out, err := json.Marshal(root)
	if err != nil {
		return data
	}
	return out
}

func usageValue(root map[string]any, key string) any {
	if u, ok := root["usage"].(map[string]any); ok {
		return u[key]
	}
	return nil
}

// guardEmptyStream repassa um stream SSE linha a linha; se terminar sem nenhum
// delta.content/tool_calls, injeta um chunk de aviso antes do [DONE]. Loga o
// resumo do stream ao final.
func guardEmptyStream(body io.ReadCloser, model any, logger *slog.Logger) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		defer body.Close()
		br := bufio.NewReader(body)

		hasContent, sawDone, sawReasoning := false, false, false
		finish := ""
		contentLen := 0
		warn := func() error {
			if hasContent {
				return nil
			}
			msg := fmt.Sprintf("⚠ O modelo não retornou conteúdo (finish_reason=%s).", finish)
			chunk, _ := json.Marshal(map[string]any{
				"id":     "proxy-warn",
				"object": "chat.completion.chunk",
				"choices": []any{map[string]any{
					"index":         0,
					"delta":         map[string]any{"role": "assistant", "content": msg},
					"finish_reason": "stop",
				}},
			})
			hasContent = true
			_, err := pw.Write(append(append([]byte("data: "), chunk...), "\n\n"...))
			return err
		}

		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				trimmed := bytes.TrimSpace(line)
				if payload, ok := bytes.CutPrefix(trimmed, []byte("data:")); ok {
					payload = bytes.TrimSpace(payload)
					if string(payload) == "[DONE]" {
						sawDone = true
						if werr := warn(); werr != nil {
							pw.CloseWithError(werr)
							return
						}
					} else {
						n, reasoning, f, tools := inspectChunk(payload)
						contentLen += n
						if n > 0 || tools {
							hasContent = true
						}
						sawReasoning = sawReasoning || reasoning
						if f != "" {
							finish = f
						}
					}
				}
				if _, werr := pw.Write(line); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					pw.CloseWithError(err)
					return
				}
				break
			}
		}

		if !sawDone {
			if werr := warn(); werr != nil {
				return
			}
		}
		logger.Info("chat_completion_stream", "model", model, "finish_reason", finish,
			"content_len", contentLen, "has_content", hasContent, "saw_reasoning", sawReasoning)
		pw.Close()
	}()

	return &guardedBody{ReadCloser: pr, once: sync.Once{}, upstream: body}
}

type guardedBody struct {
	io.ReadCloser
	once     sync.Once
	upstream io.Closer
}

func (g *guardedBody) Close() error {
	g.once.Do(func() { _ = g.upstream.Close() })
	return g.ReadCloser.Close()
}

// inspectChunk extrai de um chunk SSE o tamanho do content, se há reasoning,
// o finish_reason e se há tool_calls.
func inspectChunk(payload []byte) (contentLen int, reasoning bool, finish string, tools bool) {
	var c struct {
		Choices []struct {
			Delta        map[string]any `json:"delta"`
			FinishReason string         `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(payload, &c) != nil {
		return
	}
	for _, ch := range c.Choices {
		if s, _ := ch.Delta["content"].(string); s != "" {
			contentLen += len(s)
		}
		if tc, _ := ch.Delta["tool_calls"].([]any); len(tc) > 0 {
			tools = true
		}
		for _, k := range reasoningKeys {
			if s, _ := ch.Delta[k].(string); s != "" {
				reasoning = true
			}
		}
		if ch.FinishReason != "" {
			finish = ch.FinishReason
		}
	}
	return
}

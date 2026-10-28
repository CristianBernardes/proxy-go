package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// sseToOllamaNDJSON converte um stream SSE OpenAI em linhas NDJSON no formato
// Ollama (/api/chat ou, se generate, /api/generate). O último objeto traz
// "done":true.
func sseToOllamaNDJSON(body io.ReadCloser, model string, generate bool) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		defer body.Close()
		br := bufio.NewReader(body)
		doneSent := false

		emit := func(content, role string, done bool) error {
			obj := map[string]any{
				"model":      model,
				"created_at": time.Now().Format(time.RFC3339Nano),
				"done":       done,
			}
			if generate {
				obj["response"] = content
			} else {
				obj["message"] = map[string]any{"role": role, "content": content}
			}
			if done {
				obj["done_reason"] = "stop"
				doneSent = true
			}
			b, _ := json.Marshal(obj)
			_, err := pw.Write(append(b, '\n'))
			return err
		}

		for {
			line, err := br.ReadBytes('\n')
			if payload, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:")); ok {
				payload = bytes.TrimSpace(payload)
				if string(payload) == "[DONE]" {
					if !doneSent {
						if werr := emit("", "assistant", true); werr != nil {
							return
						}
					}
				} else {
					var c struct {
						Choices []struct {
							Delta struct {
								Role    string `json:"role"`
								Content string `json:"content"`
							} `json:"delta"`
						} `json:"choices"`
					}
					if json.Unmarshal(payload, &c) == nil && len(c.Choices) > 0 && c.Choices[0].Delta.Content != "" {
						role := c.Choices[0].Delta.Role
						if role == "" {
							role = "assistant"
						}
						if werr := emit(c.Choices[0].Delta.Content, role, false); werr != nil {
							return
						}
					}
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
		if !doneSent {
			_ = emit("", "assistant", true)
		}
		pw.Close()
	}()

	return pr
}

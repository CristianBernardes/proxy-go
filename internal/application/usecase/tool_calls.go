package usecase

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// toolNames extrai os nomes das funções declaradas em "tools" do payload.
func toolNames(payload map[string]any) map[string]bool {
	names := map[string]bool{}
	tools, ok := payload["tools"].([]any)
	if !ok {
		return names
	}
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			continue
		}
		if n, ok := fn["name"].(string); ok {
			names[n] = true
		}
	}
	return names
}

// normalizeToolCalls converte tool calls emitidas como texto (ex.: Qwen no
// Ollama) para o formato estruturado "tool_calls" do OpenAI, e garante que
// tool calls já estruturadas tenham arguments como string. Retorna o JSON
// original inalterado se nada precisar ser ajustado.
func normalizeToolCalls(data []byte, names map[string]bool) []byte {
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

		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			normalizeExistingToolCalls(tc)
			ch["finish_reason"] = "tool_calls"
			changed = true
			continue
		}

		content, _ := msg["content"].(string)
		calls := parseTextToolCalls(content, names)
		if len(calls) > 0 {
			msg["tool_calls"] = calls
			msg["content"] = ""
			ch["finish_reason"] = "tool_calls"
			changed = true
		}
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

// parseTextToolCalls tenta interpretar o conteúdo textual como uma (ou várias)
// tool calls no formato {"name":..., "arguments":...}, aceitando objeto único
// ou array. Só considera nomes declarados em names (quando houver).
func parseTextToolCalls(content string, names map[string]bool) []any {
	s := strings.TrimSpace(content)
	if s == "" || (s[0] != '{' && s[0] != '[') {
		return nil
	}

	var calls []any
	add := func(m map[string]any) {
		name, _ := m["name"].(string)
		if name == "" || (len(names) > 0 && !names[name]) {
			return
		}
		args := m["arguments"]
		if args == nil {
			args = m["parameters"]
		}
		argStr := "{}"
		if args != nil {
			if b, err := json.Marshal(args); err == nil {
				argStr = string(b)
			}
		}
		calls = append(calls, map[string]any{
			"id":       "call_" + randID(),
			"type":     "function",
			"index":    len(calls),
			"function": map[string]any{"name": name, "arguments": argStr},
		})
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		add(obj)
		return calls
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		for _, m := range arr {
			add(m)
		}
		return calls
	}
	return nil
}

// normalizeExistingToolCalls garante o formato OpenAI em tool calls que já
// vieram estruturadas: type "function", id presente, index no topo e arguments
// como string JSON.
func normalizeExistingToolCalls(tc []any) {
	for i, item := range tc {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == nil {
			m["type"] = "function"
		}
		if id, _ := m["id"].(string); id == "" {
			m["id"] = "call_" + randID()
		}
		if _, ok := m["index"]; !ok {
			m["index"] = i
		}
		fn, ok := m["function"].(map[string]any)
		if !ok {
			continue
		}
		delete(fn, "index")
		if a, ok := fn["arguments"]; ok {
			if _, isStr := a.(string); !isStr {
				if b, err := json.Marshal(a); err == nil {
					fn["arguments"] = string(b)
				}
			}
		}
	}
}

// chatCompletionToSSE converte uma resposta chat.completion (non-stream) em um
// stream SSE de um único chunk seguido de "[DONE]", movendo "message" para
// "delta" — formato aceito por clientes OpenAI compatíveis.
func chatCompletionToSSE(data []byte) []byte {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		var buf bytes.Buffer
		buf.WriteString("data: ")
		buf.Write(data)
		buf.WriteString("\n\ndata: [DONE]\n\n")
		return buf.Bytes()
	}

	root["object"] = "chat.completion.chunk"
	if choices, ok := root["choices"].([]any); ok {
		for _, c := range choices {
			ch, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if msg, ok := ch["message"].(map[string]any); ok {
				ch["delta"] = msg
				delete(ch, "message")
			}
		}
	}

	chunk, err := json.Marshal(root)
	if err != nil {
		chunk = data
	}
	var buf bytes.Buffer
	buf.WriteString("data: ")
	buf.Write(chunk)
	buf.WriteString("\n\ndata: [DONE]\n\n")
	return buf.Bytes()
}

// randID gera um identificador hexadecimal curto para tool calls.
func randID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

package usecase

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestEnsureContentPromotesReasoning(t *testing.T) {
	in := `{"model":"m","choices":[{"message":{"role":"assistant","content":"","reasoning":"pensando"},"finish_reason":"stop"}]}`
	var out struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(ensureContent([]byte(in), testLogger), &out); err != nil {
		t.Fatal(err)
	}
	msg := out.Choices[0].Message
	if msg["content"] != "pensando" || msg["reasoning"] != nil {
		t.Fatalf("msg inesperada: %v", msg)
	}
}

func TestEnsureContentWarnsWhenEmpty(t *testing.T) {
	in := `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`
	if got := string(ensureContent([]byte(in), testLogger)); !strings.Contains(got, "finish_reason=length") {
		t.Fatalf("sem aviso: %s", got)
	}
}

func TestEnsureContentKeepsToolCalls(t *testing.T) {
	in := `{"choices":[{"message":{"content":"","tool_calls":[{"id":"1"}]},"finish_reason":"tool_calls"}]}`
	if got := string(ensureContent([]byte(in), testLogger)); strings.Contains(got, "⚠") {
		t.Fatalf("não deveria avisar: %s", got)
	}
}

func readAll(t *testing.T, s string) string {
	t.Helper()
	rc := guardEmptyStream(io.NopCloser(strings.NewReader(s)), "m", testLogger)
	b, _ := io.ReadAll(rc)
	return string(b)
}

func TestGuardEmptyStreamInjectsWarning(t *testing.T) {
	out := readAll(t, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"x\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	if !strings.Contains(out, "⚠ O modelo não retornou conteúdo (finish_reason=stop)") || !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Fatalf("saída: %q", out)
	}
}

func TestGuardEmptyStreamPassThrough(t *testing.T) {
	in := "data: {\"choices\":[{\"delta\":{\"content\":\"oi\"}}]}\n\ndata: [DONE]\n\n"
	if out := readAll(t, in); out != in {
		t.Fatalf("alterou stream válido: %q", out)
	}
}

func TestLastUserPromptParts(t *testing.T) {
	msgs := []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "text", "text": "olá"},
		map[string]any{"type": "image_url"},
		map[string]any{"type": "text", "text": "mundo"},
	}}}
	if got := lastUserPrompt(msgs); got != "olá\nmundo" {
		t.Fatalf("got %q", got)
	}
}

func TestHistorySummary(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "a"},
		map[string]any{"role": "assistant", "content": "b"},
		map[string]any{"role": "user", "content": "c"},
	}
	if got := historySummary(msgs); got != "user: a\nassistant: b" {
		t.Fatalf("got %q", got)
	}
}

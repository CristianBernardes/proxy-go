package streaming

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// slowChunks entrega cada pedaço com um intervalo, simulando um upstream lento
// cujos eventos chegam partidos.
type slowChunks struct {
	chunks []string
	delay  time.Duration
	err    error
}

func (s *slowChunks) Read(p []byte) (int, error) {
	if len(s.chunks) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	n := copy(p, s.chunks[0])
	s.chunks = s.chunks[1:]
	return n, nil
}
func (s *slowChunks) Close() error { return nil }

func run(t *testing.T, ct string, keepAlive time.Duration, produce Producer) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	onErr := SSEError
	if ct != "text/event-stream" {
		onErr = NDJSONError
	}
	_ = StreamAsync(rec, req, ct, keepAlive, 4, produce, onErr)
	return rec.Body.String()
}

func TestPingOnlyBetweenCompleteEvents(t *testing.T) {
	out := run(t, "text/event-stream", 20*time.Millisecond, func(context.Context) (io.ReadCloser, error) {
		return &slowChunks{chunks: []string{`data: {"a":`, `"1"}` + "\n\n", `data: {"b":"2"}`, "\n\n"}, delay: 60 * time.Millisecond}, nil
	})
	if !strings.Contains(out, `data: {"a":"1"}`+"\n\n") || !strings.Contains(out, `data: {"b":"2"}`+"\n\n") {
		t.Fatalf("eventos corrompidos: %q", out)
	}
	if !strings.Contains(out, ": ping") {
		t.Fatalf("esperava ping: %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "data:") && !strings.HasSuffix(line, "}") {
			t.Fatalf("linha data partida: %q", line)
		}
	}
}

func TestProducerErrorAfter200(t *testing.T) {
	out := run(t, "text/event-stream", 0, func(context.Context) (io.ReadCloser, error) {
		return nil, errors.New("upstream erro 404")
	})
	if !strings.Contains(out, "⚠ proxy-go: upstream erro 404") || !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Fatalf("erro não visível: %q", out)
	}
}

func TestReadErrorMidStream(t *testing.T) {
	out := run(t, "text/event-stream", 0, func(context.Context) (io.ReadCloser, error) {
		return &slowChunks{chunks: []string{"data: {\"x\":1}\n\n", "data: {\"par"}, err: errors.New("boom")}, nil
	})
	if !strings.Contains(out, `data: {"x":1}`) || !strings.Contains(out, "⚠ proxy-go: boom") || strings.Contains(out, `{"par`) {
		t.Fatalf("saída inesperada: %q", out)
	}
}

func TestNDJSONNoPing(t *testing.T) {
	out := run(t, "application/x-ndjson", 10*time.Millisecond, func(context.Context) (io.ReadCloser, error) {
		return &slowChunks{chunks: []string{`{"a":1}`, "\n"}, delay: 50 * time.Millisecond}, nil
	})
	if strings.Contains(out, "ping") || out != "{\"a\":1}\n" {
		t.Fatalf("saída inesperada: %q", out)
	}
}

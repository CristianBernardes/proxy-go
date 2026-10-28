package streaming

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const (
	contentTypeSSE = "text/event-stream"
	sseDone        = "data: [DONE]\n\n"
)

// Producer obtém o corpo upstream. É executado depois que o cabeçalho 200 já
// foi enviado ao cliente, então não bloqueia o primeiro byte.
type Producer func(ctx context.Context) (io.ReadCloser, error)

type event struct {
	data []byte
	err  error
}

// StreamAsync envia os headers (200 + flush) imediatamente, executa produce em
// background e repassa o corpo ao cliente com um ÚNICO escritor: só eventos
// completos (terminados em "\n\n" no SSE, "\n" no NDJSON) são escritos, e o
// ping de keep-alive (somente SSE) só entra entre eventos, quando ocioso.
//
// Falhas depois do 200 viram um evento visível ao usuário via onError.
// bufSize é a profundidade do channel entre leitor e escritor (backpressure).
func StreamAsync(w http.ResponseWriter, r *http.Request, contentType string, keepAlive time.Duration, bufSize int, produce Producer, onError func(error) []byte) error {
	if bufSize <= 0 {
		bufSize = 64
	}
	sse := contentType == contentTypeSSE
	delim := []byte("\n")
	if sse {
		delim = []byte("\n\n")
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	events := make(chan event, bufSize)
	go func() {
		defer close(events)
		send := func(ev event) bool {
			select {
			case events <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}

		body, err := produce(ctx)
		if err != nil {
			send(event{err: err})
			return
		}
		defer body.Close()

		reader := make([]byte, 4096)
		for {
			n, err := body.Read(reader)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, reader[:n])
				if !send(event{data: chunk}) {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					send(event{err: err})
				}
				return
			}
		}
	}()

	var tick <-chan time.Time
	if sse && keepAlive > 0 {
		ticker := time.NewTicker(keepAlive)
		defer ticker.Stop()
		tick = ticker.C
	}

	var pending []byte
	lastWrite := time.Now()
	write := func(p []byte) error {
		if len(p) == 0 {
			return nil
		}
		if _, err := w.Write(p); err != nil {
			return err
		}
		flush()
		lastWrite = time.Now()
		return nil
	}

	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return write(pending)
			}
			if ev.err != nil {
				// Evento parcial é descartado: não dá para completá-lo.
				out := []byte{}
				if onError != nil {
					out = append(out, onError(ev.err)...)
				}
				if sse {
					out = append(out, sseDone...)
				}
				if err := write(out); err != nil {
					return err
				}
				return ev.err
			}
			pending = append(pending, ev.data...)
			if cut := bytes.LastIndex(pending, delim); cut >= 0 {
				end := cut + len(delim)
				if err := write(pending[:end]); err != nil {
					return err
				}
				pending = append(pending[:0], pending[end:]...)
			}
		case <-tick:
			if time.Since(lastWrite) >= keepAlive {
				if err := write([]byte(": ping\n\n")); err != nil {
					return err
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// SSEError monta um chunk de chat.completion com o erro como texto visível.
func SSEError(err error) []byte {
	chunk := map[string]any{
		"id":     "proxy-err",
		"object": "chat.completion.chunk",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"role": "assistant", "content": "⚠ proxy-go: " + err.Error()},
			"finish_reason": "stop",
		}},
	}
	b, _ := json.Marshal(chunk)
	return append(append([]byte("data: "), b...), "\n\n"...)
}

// NDJSONError monta a linha de erro no formato Ollama.
func NDJSONError(err error) []byte {
	b, _ := json.Marshal(map[string]any{"error": "proxy-go: " + err.Error()})
	return append(b, '\n')
}

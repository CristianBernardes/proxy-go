package httpx

import (
	"encoding/json"
	"io"
	"net/http"
)

// ParseBody decodifica o corpo JSON da requisição; retorna mapa vazio em caso de erro.
func ParseBody(r *http.Request) map[string]any {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return map[string]any{}
	}
	return body
}

// JSON serializa data como JSON e escreve no ResponseWriter com o status informado.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// Error escreve uma resposta de erro no formato padrão do proxy (OpenAI-like).
func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]any{
		"error": map[string]any{"message": message, "type": "proxy_error"},
	})
}

// CopyJSON repassa um corpo JSON já pronto (vindo do upstream) sem re-serializar,
// evitando overhead de decode/encode desnecessário.
func CopyJSON(w http.ResponseWriter, status int, body io.Reader) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.Copy(w, body)
}

package entity

import (
	"errors"
	"time"
)

// Model é a entidade de domínio que representa um modelo de IA disponível.
type Model struct {
	ID        string
	Name      string
	OwnedBy   string
	CreatedAt time.Time

	// Capabilities vem de /api/show; nil significa "desconhecido".
	Capabilities []string
}

// HasCapability informa se o modelo declara a capacidade; known=false quando
// as capabilities não puderam ser detectadas.
func (m *Model) HasCapability(name string) (has, known bool) {
	if m.Capabilities == nil {
		return false, false
	}
	for _, c := range m.Capabilities {
		if c == name {
			return true, true
		}
	}
	return false, true
}

// VSCodeLimits são os valores finais de uma entrada de "chat.models".
type VSCodeLimits struct {
	ToolCalling     bool
	Vision          bool
	MaxInputTokens  int
	MaxOutputTokens int
}

// NewModel valida e constrói um Model a partir dos dados brutos do upstream.
func NewModel(id, name, ownedBy string, createdUnix int64) (*Model, error) {
	if id == "" {
		return nil, errors.New("model: id é obrigatório")
	}
	if name == "" {
		name = id
	}
	if ownedBy == "" {
		ownedBy = "ollama"
	}

	createdAt := time.Now()
	if createdUnix > 0 {
		createdAt = time.Unix(createdUnix, 0)
	}

	return &Model{ID: id, Name: name, OwnedBy: ownedBy, CreatedAt: createdAt}, nil
}

// ToOpenAIFormat converte o modelo para o formato esperado por GET /v1/models.
func (m *Model) ToOpenAIFormat() map[string]any {
	return map[string]any{
		"id":       m.ID,
		"object":   "model",
		"created":  m.CreatedAt.Unix(),
		"owned_by": m.OwnedBy,
		"name":     m.Name,
	}
}

// ToOllamaFormat converte o modelo para o formato esperado por GET /api/tags.
func (m *Model) ToOllamaFormat() map[string]any {
	return map[string]any{
		"name":        m.ID,
		"model":       m.ID,
		"modified_at": m.CreatedAt.Format(time.RFC3339),
		"size":        0,
		"digest":      "",
		"details": map[string]any{
			"family":             m.OwnedBy,
			"parameter_size":     "",
			"quantization_level": "",
		},
	}
}

// ToVSCodeConfig converte o modelo para uma entrada de "chat.models" do VS Code.
// Os limites e capacidades são calculados pelo chamador (ver VSCodeLimits).
func (m *Model) ToVSCodeConfig(proxyURL string, lim VSCodeLimits) map[string]any {
	return map[string]any{
		"id":              m.ID,
		"name":            m.Name,
		"url":             proxyURL + "/v1/chat/completions",
		"toolCalling":     lim.ToolCalling,
		"vision":          lim.Vision,
		"maxInputTokens":  lim.MaxInputTokens,
		"maxOutputTokens": lim.MaxOutputTokens,
	}
}

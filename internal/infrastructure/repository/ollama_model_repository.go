package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"edenred-ai-proxy/internal/domain/entity"
	"edenred-ai-proxy/internal/domain/port"
	"edenred-ai-proxy/internal/infrastructure/cache"
	"edenred-ai-proxy/internal/infrastructure/config"
)

const cacheKey = "models"

// Ollama busca modelos do Ollama com cache em memória (Repository + Adapter).
type Ollama struct {
	config *config.EnvConfig
	client port.HTTPClient
	cache  *cache.TTLCache
}

// NewOllama constrói o repository com suas dependências injetadas.
func NewOllama(cfg *config.EnvConfig, client port.HTTPClient, c *cache.TTLCache) *Ollama {
	return &Ollama{config: cfg, client: client, cache: c}
}

// rawModel é o DTO que mapeia a resposta JSON do Ollama /v1/models.
type rawModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OwnedBy string `json:"owned_by"`
	Created int64  `json:"created"`
}

// GetAll busca os modelos do Ollama com cache em memória (TTL configurável).
// O upstream ora responde array puro, ora objeto {data: [...]}, então tentamos
// ambos os formatos antes de falhar.
func (r *Ollama) GetAll(ctx context.Context) ([]*entity.Model, error) {
	if cached, ok := r.cache.Get(cacheKey); ok {
		return cached.([]*entity.Model), nil
	}

	resp, err := r.client.Do(ctx, port.Request{
		Method: "GET",
		URL:    r.config.OllamaURL + "/v1/models",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.Status >= 400 {
		return nil, fmt.Errorf("upstream %d", resp.Status)
	}

	// O upstream ora responde um array puro, ora um objeto { data: [...] }.
	var raw []rawModel
	if err := json.Unmarshal(data, &raw); err != nil {
		var wrapped struct {
			Data []rawModel `json:"data"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return nil, fmt.Errorf("resposta de modelos inválida: %w", err)
		}
		raw = wrapped.Data
	}

	models := make([]*entity.Model, 0, len(raw))
	for _, m := range raw {
		model, err := entity.NewModel(m.ID, m.Name, m.OwnedBy, m.Created)
		if err != nil {
			continue
		}
		models = append(models, model)
	}

	r.loadCapabilities(ctx, models)

	r.cache.Set(cacheKey, models, r.config.ModelsCacheTTL)
	return models, nil
}

// loadCapabilities consulta POST /api/show por modelo (em paralelo) e preenche
// Capabilities. Falhas deixam o campo nil (desconhecido).
func (r *Ollama) loadCapabilities(ctx context.Context, models []*entity.Model) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, m := range models {
		wg.Add(1)
		go func(m *entity.Model) {
			defer wg.Done()
			body, _ := json.Marshal(map[string]any{"model": m.ID})
			resp, err := r.client.Do(ctx, port.Request{
				Method: http.MethodPost,
				URL:    r.config.OllamaURL + "/api/show",
				Body:   body,
			})
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.Status >= 400 {
				return
			}
			var show struct {
				Capabilities []string `json:"capabilities"`
			}
			if json.NewDecoder(resp.Body).Decode(&show) == nil && show.Capabilities != nil {
				m.Capabilities = show.Capabilities
			}
		}(m)
	}
	wg.Wait()
}

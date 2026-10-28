package port

import (
	"context"

	"edenred-ai-proxy/internal/domain/entity"
)

// ModelRepository abstrai a origem dos modelos de IA disponíveis.
type ModelRepository interface {
	GetAll(ctx context.Context) ([]*entity.Model, error)
}

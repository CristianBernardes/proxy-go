package usecase

import (
	"context"

	"edenred-ai-proxy/internal/domain/entity"
	"edenred-ai-proxy/internal/domain/port"
)

// GetModels retorna todos os modelos disponíveis, com cache transparente
// delegado ao ModelRepository.
type GetModels struct {
	repo port.ModelRepository
}

// NewGetModels constrói o caso de uso com o repository injetado.
func NewGetModels(repo port.ModelRepository) *GetModels {
	return &GetModels{repo: repo}
}

// Execute retorna todos os modelos disponíveis, delegando cache ao repository.
func (uc *GetModels) Execute(ctx context.Context) ([]*entity.Model, error) {
	return uc.repo.GetAll(ctx)
}

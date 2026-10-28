package bootstrap

import (
	"log/slog"
	"net/http"
	"strings"

	"edenred-ai-proxy/internal/application/usecase"
	"edenred-ai-proxy/internal/domain/port"
	"edenred-ai-proxy/internal/infrastructure/cache"
	"edenred-ai-proxy/internal/infrastructure/config"
	"edenred-ai-proxy/internal/infrastructure/httpclient"
	"edenred-ai-proxy/internal/infrastructure/repository"
	"edenred-ai-proxy/internal/presentation"
	"edenred-ai-proxy/internal/shared/logger"
)

// Container monta e mantém o grafo de dependências da aplicação — único
// lugar do sistema que conhece os tipos concretos (Composition Root).
type Container struct {
	Config *config.EnvConfig
	Logger *slog.Logger

	GetModels            *usecase.GetModels
	ChatCompletion       *usecase.ChatCompletion
	Ensemble             *usecase.Ensemble
	GenerateVSCodeConfig *usecase.GenerateVSCodeConfig

	Router http.Handler
}

// NewContainer constrói o grafo de dependências a partir do .env em envPath.
func NewContainer(envPath string) (*Container, error) {
	cfg, err := config.Load(envPath)
	if err != nil {
		return nil, err
	}

	log := logger.Get()
	memCache := cache.NewTTLCache()
	httpClient := httpclient.New(cfg.InsecureTLS && strings.HasPrefix(cfg.OllamaURL, "https://"))

	var modelRepo port.ModelRepository = repository.NewOllama(cfg, httpClient, memCache)

	getModels := usecase.NewGetModels(modelRepo)
	chatCompletion := usecase.NewChatCompletion(cfg, httpClient, log)
	ensemble := usecase.NewEnsemble(cfg, chatCompletion, getModels, log)
	generateConfig := usecase.NewGenerateVSCodeConfig(getModels, cfg, log)

	router := presentation.NewRouter(getModels, chatCompletion, ensemble, generateConfig, cfg, log)

	return &Container{
		Config: cfg,
		Logger: log,

		GetModels:            getModels,
		ChatCompletion:       chatCompletion,
		Ensemble:             ensemble,
		GenerateVSCodeConfig: generateConfig,

		Router: router,
	}, nil
}

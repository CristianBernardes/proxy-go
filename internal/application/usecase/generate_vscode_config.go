package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"edenred-ai-proxy/internal/domain/entity"

	"edenred-ai-proxy/internal/infrastructure/config"
)

// VSCodeModelGroup é uma entrada de "chat.models" pronta para o settings.json do VS Code.
type VSCodeModelGroup struct {
	Name    string           `json:"name"`
	Vendor  string           `json:"vendor"`
	APIKey  string           `json:"apiKey,omitempty"`
	APIType string           `json:"apiType"`
	Models  []map[string]any `json:"models"`
}

// GenerateVSCodeConfig busca os modelos e gera o JSON de configuração do VS Code.
type GenerateVSCodeConfig struct {
	getModels *GetModels
	config    *config.EnvConfig
	logger    *slog.Logger
}

// NewGenerateVSCodeConfig constrói o caso de uso com suas dependências injetadas.
func NewGenerateVSCodeConfig(getModels *GetModels, cfg *config.EnvConfig, logger *slog.Logger) *GenerateVSCodeConfig {
	return &GenerateVSCodeConfig{getModels: getModels, config: cfg, logger: logger}
}

// GenerateOptions controla a saída da geração de configuração — OutputPath
// grava o JSON em disco, Print exibe no stdout.
type GenerateOptions struct {
	OutputPath string
	Print      bool
}

// Execute busca os modelos disponíveis e gera o JSON de configuração do VS Code
// ("chat.models"), opcionalmente gravando em disco e/ou exibindo no stdout.
func (uc *GenerateVSCodeConfig) Execute(ctx context.Context, opts GenerateOptions) ([]VSCodeModelGroup, error) {
	models, err := uc.getModels.Execute(ctx)
	if err != nil {
		return nil, err
	}

	proxyURL := uc.config.ProxyURL()
	overrides := loadOverrides(uc.config.OverridesPath, uc.logger)
	modelConfigs := make([]map[string]any, 0, len(models))
	for _, m := range models {
		modelConfigs = append(modelConfigs, m.ToVSCodeConfig(proxyURL, computeLimits(m, uc.config.NumCtx, overrides[m.ID])))
	}

	chatModels := []VSCodeModelGroup{{
		Name:    "Ollama",
		Vendor:  "customendpoint",
		APIType: "chat-completions",
		Models:  modelConfigs,
	}}

	if opts.OutputPath != "" {
		if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(chatModels, "", "    ")
		if err := os.WriteFile(opts.OutputPath, data, 0o644); err != nil {
			return nil, err
		}
		uc.logger.Info("generate_config", "arquivo", opts.OutputPath)
	}

	if opts.Print {
		fmt.Printf("\nEncontrados %d modelos:\n\n", len(models))
		for _, m := range models {
			fmt.Printf("  • %s  [%s]\n", m.Name, m.ID)
		}
		fmt.Println("\n─────────────────────────────────────────────────────")
		fmt.Println("Adicione ao VS Code settings.json (Ctrl+Shift+P → Open User Settings JSON):")
		fmt.Println("─────────────────────────────────────────────────────")
		data, _ := json.MarshalIndent(chatModels, "", "    ")
		fmt.Println(`"chat.models":`)
		fmt.Println(string(data))
		fmt.Println("─────────────────────────────────────────────────────")
		fmt.Println("IMPORTANTE: inicie o proxy antes (make run / make start)")
	}

	return chatModels, nil
}

// ModelOverride permite ajustar manualmente um modelo em models.overrides.json
// (objeto indexado pelo id do modelo).
type ModelOverride struct {
	MaxInputTokens  int   `json:"maxInputTokens"`
	MaxOutputTokens int   `json:"maxOutputTokens"`
	ToolCalling     *bool `json:"toolCalling"`
	Vision          *bool `json:"vision"`
}

func loadOverrides(path string, logger *slog.Logger) map[string]ModelOverride {
	out := map[string]ModelOverride{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		logger.Warn("generate_config", "overrides", path, "erro", err.Error())
	}
	return out
}

// computeLimits aplica capabilities detectadas, depois o override, e garante
// que maxInputTokens + maxOutputTokens nunca ultrapasse numCtx.
func computeLimits(m *entity.Model, numCtx int, ov ModelOverride) entity.VSCodeLimits {
	toolCalling := true
	if has, known := m.HasCapability("tools"); known {
		toolCalling = has
	}
	vision, known := m.HasCapability("vision")
	if !known {
		vision = strings.Contains(strings.ToLower(m.ID), "vision")
	}
	if ov.ToolCalling != nil {
		toolCalling = *ov.ToolCalling
	}
	if ov.Vision != nil {
		vision = *ov.Vision
	}

	output := ov.MaxOutputTokens
	if output <= 0 {
		output = min(8192, numCtx/4)
	}
	if output >= numCtx {
		output = numCtx / 4
	}
	input := ov.MaxInputTokens
	if input <= 0 || input+output > numCtx {
		input = numCtx - output
	}

	return entity.VSCodeLimits{ToolCalling: toolCalling, Vision: vision, MaxInputTokens: input, MaxOutputTokens: output}
}

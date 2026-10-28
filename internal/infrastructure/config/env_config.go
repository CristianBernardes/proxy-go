package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvConfig lê e expõe a configuração do .env sem dependências externas.
type EnvConfig struct {
	OllamaURL         string
	Host              string
	InsecureTLS       bool
	NumCtx            int
	OverridesPath     string
	Port              string
	ModelsCacheTTL    time.Duration
	StreamBufferSize  int
	KeepAliveInterval time.Duration
	ShutdownTimeout   time.Duration

	// Orquestração de Consenso (Mixture of Agents).
	EnsembleEnabled     bool
	EnsembleAlways      bool
	EnsembleModel       string
	EnsembleWorkers     []string
	EnsembleJudge       string
	EnsembleMaxWorkers  int
	EnsembleTimeout     time.Duration
	EnsembleConcurrency int
}

// Load lê o arquivo em envPath (se existir) e mescla com variáveis de
// ambiente do processo, que sempre têm prioridade sobre o arquivo.
func Load(envPath string) (*EnvConfig, error) {
	values := map[string]string{}

	if f, err := os.Open(envPath); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			sep := strings.Index(line, "=")
			if sep == -1 {
				continue
			}
			key := strings.TrimSpace(line[:sep])
			val := strings.Trim(strings.TrimSpace(line[sep+1:]), `"'`)
			values[key] = val
		}
	}

	get := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v, ok := values[key]; ok && v != "" {
			return v
		}
		return def
	}

	ttlSeconds, _ := strconv.Atoi(get("MODELS_CACHE_TTL", "300"))
	bufSize, _ := strconv.Atoi(get("STREAM_BUFFER_SIZE", "64"))
	keepAliveSeconds, _ := strconv.Atoi(get("KEEPALIVE_INTERVAL_SECONDS", "15"))
	shutdownSeconds, _ := strconv.Atoi(get("SHUTDOWN_TIMEOUT_SECONDS", "10"))

	ensembleEnabled := strings.EqualFold(get("ENSEMBLE_ENABLED", "false"), "true")
	ensembleAlways := strings.EqualFold(get("ENSEMBLE_ALWAYS", "false"), "true")
	ensembleTimeout, _ := strconv.Atoi(get("ENSEMBLE_TIMEOUT_SECONDS", "60"))
	ensembleMaxWorkers, _ := strconv.Atoi(get("ENSEMBLE_MAX_WORKERS", "4"))
	ensembleConcurrency, _ := strconv.Atoi(get("ENSEMBLE_CONCURRENCY", "1"))
	numCtx, _ := strconv.Atoi(get("NUM_CTX", "32768"))
	if numCtx <= 0 {
		numCtx = 32768
	}
	ensembleWorkers := splitCSV(get("ENSEMBLE_WORKERS", ""))

	return &EnvConfig{
		OllamaURL:         get("OLLAMA_URL", "http://localhost:11434"),
		Host:              get("HOST", "127.0.0.1"),
		InsecureTLS:       strings.EqualFold(get("INSECURE_TLS", "false"), "true"),
		NumCtx:            numCtx,
		OverridesPath:     get("MODELS_OVERRIDES_FILE", "models.overrides.json"),
		Port:              get("PORT", "11435"),
		ModelsCacheTTL:    time.Duration(ttlSeconds) * time.Second,
		StreamBufferSize:  bufSize,
		KeepAliveInterval: time.Duration(keepAliveSeconds) * time.Second,
		ShutdownTimeout:   time.Duration(shutdownSeconds) * time.Second,

		EnsembleEnabled:     ensembleEnabled,
		EnsembleAlways:      ensembleAlways,
		EnsembleModel:       get("ENSEMBLE_MODEL", "moa-consensus"),
		EnsembleWorkers:     ensembleWorkers,
		EnsembleJudge:       get("ENSEMBLE_JUDGE", ""),
		EnsembleMaxWorkers:  ensembleMaxWorkers,
		EnsembleTimeout:     time.Duration(ensembleTimeout) * time.Second,
		EnsembleConcurrency: ensembleConcurrency,
	}, nil
}

// ProxyURL retorna a URL local pela qual este proxy é acessado.
func (c *EnvConfig) ProxyURL() string {
	return "http://localhost:" + c.Port
}

// splitCSV divide "a, b ,c" em ["a","b","c"], ignorando itens vazios.
func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

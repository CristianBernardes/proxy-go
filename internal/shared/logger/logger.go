package logger

import (
	"log/slog"
	"os"
	"sync"
)

var (
	instance *slog.Logger
	once     sync.Once
)

// Get retorna o logger singleton do processo (Singleton pattern, idiomático via
// sync.Once). Configurado com TextHandler e nível Info.
func Get() *slog.Logger {
	once.Do(func() {
		instance = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	})
	return instance
}

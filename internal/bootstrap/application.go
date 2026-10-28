package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"edenred-ai-proxy/internal/application/usecase"
)

// Run é o ponto de entrada orquestrado pelo cmd/proxy — modo servidor ou CLI --generate-config.
func Run(generateConfig bool) {
	wd, _ := os.Getwd()
	envPath := filepath.Join(wd, ".env")

	container, err := NewContainer(envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "boot:", err)
		os.Exit(1)
	}

	if generateConfig {
		runGenerateConfig(container)
		return
	}

	runServer(container)
}

func runGenerateConfig(c *Container) {
	// Execução nativa: grava direto na raiz do projeto (cwd).
	outputPath := filepath.Join("vscode-chat-models.json")

	if _, err := c.GenerateVSCodeConfig.Execute(context.Background(), usecase.GenerateOptions{OutputPath: outputPath, Print: true}); err != nil {
		fmt.Fprintln(os.Stderr, "generate-config:", err)
		os.Exit(1)
	}
}

// runServer inicia o servidor HTTP e aguarda SIGTERM/SIGINT para graceful
// shutdown, drenando streams ativos antes de encerrar.
func runServer(c *Container) {
	addr := net.JoinHostPort(c.Config.Host, c.Config.Port)
	server := &http.Server{Addr: addr, Handler: c.Router}

	go func() {
		printBanner(c.Config.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.Logger.Error("http", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), c.Config.ShutdownTimeout)
	defer cancel()
	_ = server.Shutdown(ctx) // aguarda streams ativos drenarem antes de encerrar
}

// printBanner exibe o banner ASCII de inicialização com as rotas disponíveis.
func printBanner(port string) {
	url := "http://localhost:" + port
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║               proxy-go — rodando                  ║")
	fmt.Printf("║  %-48s║\n", url)
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  GET  %s/v1/models\n", url)
	fmt.Printf("  POST %s/v1/chat/completions\n", url)
	fmt.Printf("  GET  %s/api/tags\n", url)
	fmt.Printf("  POST %s/api/chat\n", url)
	fmt.Printf("  POST %s/api/generate\n", url)
	fmt.Printf("  GET  %s/api/ps\n", url)
	fmt.Printf("  GET  %s/vscode-config\n", url)
	fmt.Println()
}

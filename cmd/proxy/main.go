package main

import (
	"flag"

	"edenred-ai-proxy/internal/bootstrap"
)

// main é o entrypoint do servidor proxy — aceita --generate-config para
// modo CLI (gera vscode-chat-models.json e sai) ou roda o servidor HTTP.
func main() {
	generateConfig := flag.Bool("generate-config", false, "gera vscode-chat-models.json e sai")
	flag.Parse()

	bootstrap.Run(*generateConfig)
}

# Executa o proxy Go 100% localmente — sem Docker, sem containers.
# Requer apenas Go instalado no host. Uso: "make <alvo>"

BIN       := bin/proxy
USER_UNIT := $(HOME)/.config/systemd/user/proxy-go.service

.PHONY: help build run start stop restart status logs generate-config install-service uninstall-service service-logs

help:
	@echo "Execução local (binário nativo, sem Docker):"
	@echo "make build            - compila o binário em ./bin/proxy"
	@echo "make run              - roda o proxy em primeiro plano (go run), sem instalar serviço"
	@echo "make start            - inicia o serviço systemd do proxy em background"
	@echo "make stop             - para o serviço systemd do proxy"
	@echo "make restart          - para, recompila e sobe o serviço novamente (stop + build + start)"
	@echo "make status           - mostra o status do serviço systemd"
	@echo "make logs             - segue os logs do serviço em tempo real"
	@echo "make generate-config  - gera vscode-chat-models.json (nesta pasta)"
	@echo ""
	@echo "Instalação do serviço (início automático no boot):"
	@echo "make install-service  - compila, instala e habilita o serviço systemd"
	@echo "make uninstall-service- remove o serviço systemd"

# Compila o binário estático do proxy.
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN) ./cmd/proxy

# Roda o proxy em primeiro plano, direto do código-fonte — útil para
# desenvolvimento rápido, sem precisar compilar nem tocar no serviço systemd.
run:
	go run ./cmd/proxy

# Inicia o serviço systemd já instalado (ver install-service).
start:
	systemctl --user start proxy-go.service
	@echo "Serviço iniciado. Status: make status | Logs: make logs"

# Para o serviço systemd, sem desinstalá-lo.
stop:
	systemctl --user stop proxy-go.service

# Ciclo completo de atualização: para o serviço, recompila o binário com o
# código atual e sobe o serviço de novo — tudo local, sem Docker.
restart: stop build
	systemctl --user start proxy-go.service
	@echo "Proxy recompilado e reiniciado. Status: make status | Logs: make logs"

# Mostra o estado atual do serviço (ativo, PID, últimas linhas de log).
status:
	systemctl --user status proxy-go.service --no-pager -l

logs:
	journalctl --user -u proxy-go -f

# Gera vscode-chat-models.json a partir dos modelos atuais do Ollama,
# rodando o binário nativo em modo CLI (--generate-config).
generate-config: build
	./$(BIN) --generate-config

# Instala o proxy como serviço de usuário do systemd e habilita início no boot
# (enable-linger permite o serviço subir sem login interativo).
install-service: build
	mkdir -p $(HOME)/.config/systemd/user
	sed 's|__PROJECT_DIR__|$(CURDIR)|g' deploy/proxy-go.service > $(USER_UNIT)
	systemctl --user daemon-reload
	systemctl --user enable --now proxy-go.service
	loginctl enable-linger $(USER)
	@echo "Serviço instalado. Logs: make logs"

uninstall-service:
	-systemctl --user disable --now proxy-go.service
	rm -f $(USER_UNIT)
	systemctl --user daemon-reload

service-logs: logs

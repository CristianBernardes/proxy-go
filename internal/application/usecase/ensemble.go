package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"edenred-ai-proxy/internal/infrastructure/config"
)

// Ensemble implementa a Orquestração de Consenso (Mixture of Agents):
// consulta N modelos especialistas em paralelo e envia o agregado para um
// modelo Juiz, que sintetiza a resposta final em streaming.
type Ensemble struct {
	config    *config.EnvConfig
	chat      *ChatCompletion
	getModels *GetModels
	logger    *slog.Logger
}

func NewEnsemble(cfg *config.EnvConfig, chat *ChatCompletion, getModels *GetModels, logger *slog.Logger) *Ensemble {
	return &Ensemble{config: cfg, chat: chat, getModels: getModels, logger: logger}
}

// workerResult carrega a resposta (ou erro) de um único especialista.
type workerResult struct {
	Model   string
	Content string
	Err     error
}

// Execute retorna IMEDIATAMENTE um ChatCompletionResult cujo Body é o lado de
// leitura de um io.Pipe. Uma goroutine em background executa o fan-out dos
// especialistas e, em seguida, faz o streaming da síntese do Juiz para o pipe.
//
// Retornar de imediato permite que o handler inicie o KeepAlive SSE enquanto o
// comitê ainda "pensa", e o io.Pipe garante backpressure real na fase do Juiz.
func (uc *Ensemble) Execute(ctx context.Context, body map[string]any) (*ChatCompletionResult, error) {
	judge := uc.resolveJudge(body)
	if judge == "" {
		return nil, fmt.Errorf("ensemble mal configurado: nenhum modelo Juiz disponível")
	}

	workers := uc.resolveWorkers(ctx, judge)
	if len(workers) == 0 {
		// Sem especialistas para consultar: cai no fluxo normal do próprio Juiz.
		return uc.chat.Execute(ctx, map[string]any{
			"model":    judge,
			"messages": body["messages"],
			"stream":   true,
		})
	}

	messages := body["messages"]
	userPrompt := lastUserPrompt(messages)
	history := historySummary(messages)

	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()

		answers := uc.gather(ctx, workers, messages)
		uc.logResults(judge, answers)

		judgeBody := map[string]any{
			"model":    judge,
			"messages": buildJudgeMessages(userPrompt, history, answers),
			"stream":   true,
		}

		result, err := uc.chat.Execute(ctx, judgeBody)
		if err != nil {
			uc.logger.Error("ensemble", "fase", "juiz", "erro", err.Error())
			_ = pw.CloseWithError(err)
			return
		}
		defer result.Body.Close()

		// io.Copy propaga backpressure: pw.Write bloqueia até o streaming.Pipe
		// consumir pr; se o cliente cai, pr é fechado e io.Copy retorna.
		_, _ = io.Copy(pw, result.Body)
	}()

	return &ChatCompletionResult{Status: 200, Body: pr, IsStream: true}, nil
}

// resolveJudge escolhe o modelo que dará a palavra final: o modelo pedido pelo
// cliente (exceto o modelo virtual de consenso) ou, como fallback, o Juiz fixo.
func (uc *Ensemble) resolveJudge(body map[string]any) string {
	if model, _ := body["model"].(string); model != "" && model != uc.config.EnsembleModel {
		return model
	}
	return uc.config.EnsembleJudge
}

// resolveWorkers monta a lista de especialistas dinamicamente: todos os modelos
// disponíveis no Ollama, exceto o Juiz e o modelo virtual de consenso. Se
// EnsembleWorkers estiver configurado, usa essa lista fixa como fonte. O total
// é limitado por EnsembleMaxWorkers (quando > 0).
func (uc *Ensemble) resolveWorkers(ctx context.Context, judge string) []string {
	source := uc.config.EnsembleWorkers
	if len(source) == 0 {
		models, err := uc.getModels.Execute(ctx)
		if err != nil {
			uc.logger.Warn("ensemble", "fase", "listar_modelos", "erro", err.Error())
			return nil
		}
		source = make([]string, 0, len(models))
		for _, m := range models {
			source = append(source, m.ID)
		}
	}

	workers := make([]string, 0, len(source))
	for _, name := range source {
		if name == "" || name == judge || name == uc.config.EnsembleModel {
			continue
		}
		workers = append(workers, name)
	}

	if max := uc.config.EnsembleMaxWorkers; max > 0 && len(workers) > max {
		workers = workers[:max]
	}
	return workers
}

// gather executa o fan-out: uma goroutine por especialista, com no máximo
// EnsembleConcurrency consultas simultâneas, todas limitadas por um
// context.WithTimeout compartilhado. sync.WaitGroup garante que nenhuma
// goroutine fique presa — workers que estouram o timeout retornam com erro.
func (uc *Ensemble) gather(ctx context.Context, workers []string, messages any) []workerResult {
	results := make([]workerResult, len(workers))

	fanCtx, cancel := context.WithTimeout(ctx, uc.config.EnsembleTimeout)
	defer cancel()

	limit := uc.config.EnsembleConcurrency
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)

	var wg sync.WaitGroup
	for i, model := range workers {
		wg.Add(1)
		go func(idx int, model string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-fanCtx.Done():
				results[idx] = workerResult{Model: model, Err: fanCtx.Err()}
				return
			}
			start := time.Now()
			content, err := uc.askWorker(fanCtx, model, messages)
			uc.logger.Info("ensemble", "worker", model, "duracao", time.Since(start).String(), "erro", err)
			// Cada goroutine escreve em índice exclusivo → sem mutex.
			results[idx] = workerResult{Model: model, Content: content, Err: err}
		}(i, model)
	}
	wg.Wait()

	return results
}

// askWorker consulta um especialista em modo NÃO-stream e extrai o conteúdo.
func (uc *Ensemble) askWorker(ctx context.Context, model string, messages any) (string, error) {
	res, err := uc.chat.Execute(ctx, map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return extractContent(data), nil
}

func (uc *Ensemble) logResults(judge string, answers []workerResult) {
	ok := 0
	for _, a := range answers {
		if a.Err != nil {
			uc.logger.Warn("ensemble", "worker", a.Model, "erro", a.Err.Error())
			continue
		}
		if strings.TrimSpace(a.Content) != "" {
			ok++
		}
	}
	uc.logger.Info("ensemble", "workers_total", len(answers), "workers_ok", ok, "judge", judge)
}

// buildJudgeMessages monta o prompt de síntese enviado ao Juiz.
func buildJudgeMessages(userPrompt, history string, answers []workerResult) []map[string]any {
	var sb strings.Builder
	if history != "" {
		sb.WriteString("Histórico recente da conversa:\n")
		sb.WriteString(history)
		sb.WriteString("\n---\n\n")
	}
	sb.WriteString("Pergunta original do usuário:\n")
	sb.WriteString(userPrompt)
	sb.WriteString("\n\n---\n\n")
	sb.WriteString("Respostas dos especialistas do comitê:\n\n")

	valid := 0
	for i, a := range answers {
		if a.Err != nil || strings.TrimSpace(a.Content) == "" {
			continue
		}
		valid++
		fmt.Fprintf(&sb, "### Especialista %d (%s)\n%s\n\n", i+1, a.Model, a.Content)
	}
	if valid == 0 {
		sb.WriteString("(nenhum especialista respondeu a tempo — responda diretamente com seu melhor conhecimento)\n")
	}

	system := "Você é o Juiz de um comitê de modelos de IA. Analise criticamente as " +
		"respostas dos especialistas, elimine erros e alucinações, combine os melhores " +
		"pontos e produza UMA resposta final coesa, correta e completa para o usuário. " +
		"Não mencione que houve um comitê nem cite os especialistas; responda como se " +
		"fosse a resposta definitiva."

	return []map[string]any{
		{"role": "system", "content": system},
		{"role": "user", "content": sb.String()},
	}
}

// lastUserPrompt extrai o texto da última mensagem de role "user".
func lastUserPrompt(messages any) string {
	list, ok := messages.([]any)
	if !ok {
		return ""
	}
	for i := len(list) - 1; i >= 0; i-- {
		m, ok := list[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "user" {
			return contentText(m["content"])
		}
	}
	return ""
}

// contentText aceita content como string ou como array de partes
// [{"type":"text","text":"..."}] e devolve o texto concatenado.
func contentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, p := range c {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := pm["type"].(string); t != "" && t != "text" {
				continue
			}
			if text, _ := pm["text"].(string); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

const (
	historyMessages = 6
	historyMaxChars = 500
)

// historySummary resume as últimas mensagens user/assistant ANTES da última
// pergunta, truncadas, para dar contexto ao Juiz.
func historySummary(messages any) string {
	list, ok := messages.([]any)
	if !ok {
		return ""
	}
	last := -1
	for i := len(list) - 1; i >= 0; i-- {
		if m, ok := list[i].(map[string]any); ok && m["role"] == "user" {
			last = i
			break
		}
	}
	if last <= 0 {
		return ""
	}

	var lines []string
	for _, item := range list[:last] {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
		text := strings.TrimSpace(contentText(m["content"]))
		if text == "" {
			continue
		}
		if r := []rune(text); len(r) > historyMaxChars {
			text = string(r[:historyMaxChars]) + "…"
		}
		lines = append(lines, role+": "+text)
	}
	if len(lines) > historyMessages {
		lines = lines[len(lines)-historyMessages:]
	}
	return strings.Join(lines, "\n")
}

// extractContent lê choices[0].message.content de uma resposta OpenAI não-stream.
func extractContent(data []byte) string {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ""
	}
	if len(parsed.Choices) == 0 {
		return ""
	}
	return parsed.Choices[0].Message.Content
}

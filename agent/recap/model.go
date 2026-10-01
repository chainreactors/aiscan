package recap

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
)

func (w *Worker) summarize(input string) (string, error) {
	ctx, cancel := context.WithTimeout(w.ctx, 15*time.Second)
	defer cancel()
	p, config := w.providers.Current()
	if p == nil {
		return "", fmt.Errorf("provider unavailable")
	}
	model := w.model(ctx, p, config.Model)
	call := func(model string) (string, error) {
		response, err := p.ChatCompletion(ctx, &provider.ChatCompletionRequest{
			Model: model, MaxTokens: 256,
			Messages: []*aop.Message{provider.TextMessage("system", instruction), provider.TextMessage("user", input)},
		})
		if err != nil {
			return "", err
		}
		return responseText(response)
	}
	text, err := call(model)
	var apiErr *provider.APIError
	if model != config.Model && errors.As(err, &apiErr) && (apiErr.StatusCode == 403 || apiErr.StatusCode == 404 || apiErr.Code == "model_not_found") && ctx.Err() == nil {
		w.selectedModel = config.Model
		return call(config.Model)
	}
	return text, err
}

func (w *Worker) model(ctx context.Context, p provider.Provider, fallback string) string {
	if reflect.TypeOf(p).Comparable() && p == w.selectedProvider && fallback == w.defaultModel {
		return w.selectedModel
	}
	w.selectedProvider, w.defaultModel, w.selectedModel = p, fallback, fallback
	if _, rank := modelFamily(fallback); rank > 0 {
		return fallback
	}
	lister, ok := p.(interface {
		ListModels(context.Context) ([]string, error)
	})
	if !ok {
		return fallback
	}
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	models, err := lister.ListModels(lookup)
	if err == nil {
		w.selectedModel = smallModel(models, fallback)
	}
	return w.selectedModel
}

func smallModel(models []string, fallback string) string {
	family, _ := modelFamily(fallback)
	best, score := fallback, 0
	// Stable selection is independent of the endpoint's catalog order.
	models = append([]string(nil), models...)
	sort.Strings(models)
	for _, model := range models {
		candidateFamily, rank := modelFamily(model)
		if rank == 0 {
			continue
		}
		if candidateFamily == family {
			rank += 10
		}
		if rank > score {
			best, score = model, rank
		}
	}
	return best
}

// Recognize text model families, not arbitrary IDs containing "mini". A
// catalog advertises availability, not prices; unknown names use the default.
func modelFamily(model string) (string, int) {
	name := strings.ToLower(model)
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	for _, excluded := range []string{"embedding", "audio", "realtime", "image", "tts", "transcribe", "moderation", "vision", "search", "computer-use", "deep-research"} {
		if strings.Contains(name, excluded) {
			return "", 0
		}
	}
	family := ""
	switch {
	case strings.HasPrefix(name, "gpt-"):
		family = "gpt"
		if strings.Contains(name, "-nano") {
			return family, 3
		}
		if strings.Contains(name, "-mini") {
			return family, 2
		}
	case strings.HasPrefix(name, "claude-"):
		family = "claude"
		if strings.Contains(name, "haiku") {
			return family, 2
		}
	case strings.HasPrefix(name, "gemini-"):
		family = "gemini"
		if strings.Contains(name, "flash-lite") {
			return family, 3
		}
		if strings.Contains(name, "flash") {
			return family, 2
		}
	case strings.HasPrefix(name, "deepseek-"):
		family = "deepseek"
		if strings.Contains(name, "-flash") {
			return family, 2
		}
	}
	return family, 0
}

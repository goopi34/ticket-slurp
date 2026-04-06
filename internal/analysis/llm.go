package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/teilomillet/gollm"

	"ticket-slurp/internal/config"
)

// LLMGenerator is the interface for generating text from a prompt.
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string, systemPrompt string) (string, error)
}

// GollmGenerator wraps a gollm.LLM instance.
type GollmGenerator struct {
	llm gollm.LLM
}

// NewGollmGenerator creates an LLMGenerator from the given config.
// For Azure, it returns an AzureGenerator that calls the API directly,
// working around a bug in gollm where the azure_endpoint extra header is
// not propagated to the internal HTTP client.
func NewGollmGenerator(cfg config.LLMConfig) (LLMGenerator, error) {
	if strings.ToLower(cfg.Provider) == "azure" {
		return NewAzureGenerator(cfg.Azure.Endpoint, cfg.Azure.APIKey), nil
	}

	opts, err := buildGollmOptions(cfg)
	if err != nil {
		return nil, err
	}

	l, err := gollm.NewLLM(opts...)
	if err != nil {
		return nil, fmt.Errorf("create LLM: %w", err)
	}

	return &GollmGenerator{llm: l}, nil
}

// Generate calls the LLM with the given prompt and system prompt.
func (g *GollmGenerator) Generate(ctx context.Context, prompt, systemPrompt string) (string, error) {
	p := gollm.NewPrompt(
		prompt,
		gollm.WithSystemPrompt(systemPrompt, gollm.CacheTypeEphemeral),
	)
	resp, err := g.llm.Generate(ctx, p)
	if err != nil {
		return "", fmt.Errorf("LLM generate: %w", err)
	}
	return resp, nil
}

// buildGollmOptions converts our config into gollm ConfigOptions.
// Azure is handled separately via AzureGenerator and is never passed here.
func buildGollmOptions(cfg config.LLMConfig) ([]gollm.ConfigOption, error) {
	provider := strings.ToLower(cfg.Provider)
	switch provider {
	case "openai":
		opts := []gollm.ConfigOption{
			gollm.SetProvider("openai"),
			gollm.SetAPIKey(cfg.OpenAI.APIKey),
		}
		if cfg.Model != "" {
			opts = append(opts, gollm.SetModel(cfg.Model))
		}
		return opts, nil
	case "anthropic":
		opts := []gollm.ConfigOption{
			gollm.SetProvider("anthropic"),
			gollm.SetAPIKey(cfg.Anthropic.APIKey),
		}
		if cfg.Model != "" {
			opts = append(opts, gollm.SetModel(cfg.Model))
		}
		return opts, nil
	case "ollama":
		opts := []gollm.ConfigOption{
			gollm.SetProvider("ollama"),
		}
		if cfg.Model != "" {
			opts = append(opts, gollm.SetModel(cfg.Model))
		}
		if cfg.Ollama.Endpoint != "" {
			opts = append(opts, gollm.SetOllamaEndpoint(cfg.Ollama.Endpoint))
		}
		return opts, nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider %q", cfg.Provider)
	}
}

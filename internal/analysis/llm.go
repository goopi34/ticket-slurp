package analysis

import (
	"context"
	"fmt"
	"strings"

	"github.com/teilomillet/gollm"

	"ticket-slurp/internal/config"
)

// LLMGenerator is the interface for generating text from a prompt.
// It wraps gollm.LLM for testability.
type LLMGenerator interface {
	Generate(ctx context.Context, prompt string, systemPrompt string) (string, error)
}

// GollmGenerator wraps a gollm.LLM instance.
type GollmGenerator struct {
	llm gollm.LLM
}

// NewGollmGenerator creates a GollmGenerator from the given config.
func NewGollmGenerator(cfg config.LLMConfig) (*GollmGenerator, error) {
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
func buildGollmOptions(cfg config.LLMConfig) ([]gollm.ConfigOption, error) {
	provider := strings.ToLower(cfg.Provider)
	switch provider {
	case "azure":
		return []gollm.ConfigOption{
			gollm.SetProvider("azure-openai"),
			gollm.SetModel(cfg.Azure.Deployment),
			gollm.SetAPIKey(cfg.Azure.APIKey),
			gollm.SetExtraHeaders(map[string]string{
				"azure_endpoint": cfg.Azure.Endpoint,
			}),
		}, nil
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

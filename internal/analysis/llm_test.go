package analysis

import (
	"testing"

	"ticket-slurp/internal/config"
)

func TestBuildGollmOptions_UnsupportedProvider(t *testing.T) {
	cfg := config.LLMConfig{Provider: "unsupported-provider"}
	_, err := buildGollmOptions(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider, got nil")
	}
}

func TestBuildGollmOptions_Azure(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "azure",
		Azure: config.AzureConfig{
			Endpoint:   "https://example.azure.com",
			APIKey:     "key",
			Deployment: "gpt-4o",
		},
	}
	opts, err := buildGollmOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected non-empty options for azure provider")
	}
}

func TestBuildGollmOptions_OpenAI(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
		OpenAI:   config.OpenAIConfig{APIKey: "sk-test"},
	}
	opts, err := buildGollmOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected non-empty options for openai provider")
	}
}

func TestBuildGollmOptions_Anthropic(t *testing.T) {
	cfg := config.LLMConfig{
		Provider:  "anthropic",
		Model:     "claude-3-5-sonnet-20241022",
		Anthropic: config.AnthropicConfig{APIKey: "sk-ant-test"},
	}
	opts, err := buildGollmOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected non-empty options for anthropic provider")
	}
}

func TestBuildGollmOptions_Ollama(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "ollama",
		Model:    "llama3",
		Ollama:   config.OllamaConfig{Endpoint: "http://localhost:11434"},
	}
	opts, err := buildGollmOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected non-empty options for ollama provider")
	}
}

func TestBuildGollmOptions_OllamaNoModel(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "ollama",
		// No model — should still succeed with defaults.
		Ollama: config.OllamaConfig{Endpoint: "http://localhost:11434"},
	}
	opts, err := buildGollmOptions(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) == 0 {
		t.Error("expected non-empty options")
	}
}

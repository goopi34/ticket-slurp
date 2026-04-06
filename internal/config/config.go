// Package config handles loading, validation, and access to the tool configuration.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// validProviders is the set of supported LLM providers.
var validProviders = map[string]bool{
	"azure":     true,
	"openai":    true,
	"anthropic": true,
	"ollama":    true,
}

// Config is the root configuration structure.
type Config struct {
	Slack     SlackConfig     `mapstructure:"slack"`
	Timeframe TimeframeConfig `mapstructure:"timeframe"`
	Channels  ChannelsConfig  `mapstructure:"channels"`
	LLM       LLMConfig       `mapstructure:"llm"`
	Atlassian AtlassianConfig `mapstructure:"atlassian"`
	Output    OutputConfig    `mapstructure:"output"`
}

// SlackConfig holds Slack authentication credentials.
type SlackConfig struct {
	XOXC   string `mapstructure:"xoxc"`
	XOXD   string `mapstructure:"xoxd"`
	UserID string `mapstructure:"user_id"`
}

// TimeframeConfig specifies the message date range.
// Either (Start + End) or LastDays must be set.
type TimeframeConfig struct {
	Start    string `mapstructure:"start"`     // ISO 8601 date, e.g. "2026-03-01"
	End      string `mapstructure:"end"`       // ISO 8601 date, e.g. "2026-04-03"
	LastDays int    `mapstructure:"last_days"` // alternative to explicit dates
}

// ChannelsConfig controls which conversations are included.
type ChannelsConfig struct {
	Whitelist []string `mapstructure:"whitelist"`
	Blacklist []string `mapstructure:"blacklist"`
}

// LLMConfig holds settings for the gollm AI analysis provider.
type LLMConfig struct {
	Provider  string          `mapstructure:"provider"` // azure | openai | anthropic | ollama
	Model     string          `mapstructure:"model"`
	Azure     AzureConfig     `mapstructure:"azure"`
	OpenAI    OpenAIConfig    `mapstructure:"openai"`
	Anthropic AnthropicConfig `mapstructure:"anthropic"`
	Ollama    OllamaConfig    `mapstructure:"ollama"`
}

// AzureConfig holds Azure AI Foundry specific settings.
type AzureConfig struct {
	Endpoint   string `mapstructure:"endpoint"`
	APIKey     string `mapstructure:"api_key"`
	Deployment string `mapstructure:"deployment"`
}

// OpenAIConfig holds OpenAI specific settings.
type OpenAIConfig struct {
	APIKey string `mapstructure:"api_key"`
}

// AnthropicConfig holds Anthropic specific settings.
type AnthropicConfig struct {
	APIKey string `mapstructure:"api_key"`
}

// OllamaConfig holds Ollama specific settings.
type OllamaConfig struct {
	Endpoint string `mapstructure:"endpoint"`
}

// AtlassianConfig holds Jira MCP server settings.
type AtlassianConfig struct {
	MCPURL     string `mapstructure:"mcp_url"`
	ProjectKey string `mapstructure:"project_key"`
}

// OutputConfig controls report format.
type OutputConfig struct {
	Format string `mapstructure:"format"` // markdown | json
}

// Load reads and parses the configuration from the given file path.
// It expands environment variables in string values.
func Load(path string) (*Config, error) {
	v := viper.New()

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("ticket-slurp")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
	}

	v.AutomaticEnv()

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	expandEnvVars(&cfg)

	return &cfg, nil
}

// setDefaults applies default values before unmarshalling.
func setDefaults(v *viper.Viper) {
	v.SetDefault("output.format", "markdown")
	v.SetDefault("atlassian.mcp_url", "http://localhost:3000")
	v.SetDefault("llm.provider", "azure")
	v.SetDefault("ollama.endpoint", "http://localhost:11434")
}

// expandEnvVars replaces ${ENV_VAR} placeholders in sensitive string fields.
func expandEnvVars(cfg *Config) {
	cfg.Slack.XOXC = os.ExpandEnv(cfg.Slack.XOXC)
	cfg.Slack.XOXD = os.ExpandEnv(cfg.Slack.XOXD)
	cfg.LLM.Azure.APIKey = os.ExpandEnv(cfg.LLM.Azure.APIKey)
	cfg.LLM.OpenAI.APIKey = os.ExpandEnv(cfg.LLM.OpenAI.APIKey)
	cfg.LLM.Anthropic.APIKey = os.ExpandEnv(cfg.LLM.Anthropic.APIKey)
	cfg.LLM.Azure.Endpoint = os.ExpandEnv(cfg.LLM.Azure.Endpoint)
	cfg.LLM.Ollama.Endpoint = os.ExpandEnv(cfg.LLM.Ollama.Endpoint)
}

// Validate checks that all required fields are present and internally consistent.
func (c *Config) Validate() error {
	if err := c.validateSlack(); err != nil {
		return err
	}
	if err := c.validateLLM(); err != nil {
		return err
	}
	if err := c.validateTimeframe(); err != nil {
		return err
	}
	if err := c.validateOutput(); err != nil {
		return err
	}
	if c.Atlassian.ProjectKey == "" {
		return fmt.Errorf("atlassian.project_key is required")
	}
	return nil
}

func (c *Config) validateSlack() error {
	if c.Slack.XOXC == "" {
		return fmt.Errorf("slack.xoxc is required")
	}
	if c.Slack.XOXD == "" {
		return fmt.Errorf("slack.xoxd is required")
	}
	return nil
}

func (c *Config) validateLLM() error {
	provider := strings.ToLower(c.LLM.Provider)
	if !validProviders[provider] {
		return fmt.Errorf("llm.provider %q is not valid; must be one of: azure, openai, anthropic, ollama", c.LLM.Provider)
	}
	switch provider {
	case "azure":
		if c.LLM.Azure.Endpoint == "" {
			return fmt.Errorf("llm.azure.endpoint is required when provider is azure")
		}
		if c.LLM.Azure.APIKey == "" {
			return fmt.Errorf("llm.azure.api_key is required when provider is azure")
		}
		if c.LLM.Azure.Deployment == "" {
			return fmt.Errorf("llm.azure.deployment is required when provider is azure")
		}
	case "openai":
		if c.LLM.OpenAI.APIKey == "" {
			return fmt.Errorf("llm.openai.api_key is required when provider is openai")
		}
	case "anthropic":
		if c.LLM.Anthropic.APIKey == "" {
			return fmt.Errorf("llm.anthropic.api_key is required when provider is anthropic")
		}
	case "ollama":
		if c.LLM.Ollama.Endpoint == "" {
			return fmt.Errorf("llm.ollama.endpoint is required when provider is ollama")
		}
	}
	return nil
}

func (c *Config) validateTimeframe() error {
	hasDates := c.Timeframe.Start != "" || c.Timeframe.End != ""
	hasLastDays := c.Timeframe.LastDays > 0

	if !hasDates && !hasLastDays {
		return fmt.Errorf("timeframe: either (start + end) or last_days must be set")
	}
	if hasDates && hasLastDays {
		return fmt.Errorf("timeframe: start/end and last_days are mutually exclusive")
	}
	if hasDates {
		if c.Timeframe.Start == "" {
			return fmt.Errorf("timeframe.start is required when end is set")
		}
		if c.Timeframe.End == "" {
			return fmt.Errorf("timeframe.end is required when start is set")
		}
		start, err := time.Parse("2006-01-02", c.Timeframe.Start)
		if err != nil {
			return fmt.Errorf("timeframe.start %q is not a valid date (expected YYYY-MM-DD): %w", c.Timeframe.Start, err)
		}
		end, err := time.Parse("2006-01-02", c.Timeframe.End)
		if err != nil {
			return fmt.Errorf("timeframe.end %q is not a valid date (expected YYYY-MM-DD): %w", c.Timeframe.End, err)
		}
		if !end.After(start) {
			return fmt.Errorf("timeframe.end must be after timeframe.start")
		}
	}
	return nil
}

func (c *Config) validateOutput() error {
	f := strings.ToLower(c.Output.Format)
	if f != "markdown" && f != "json" {
		return fmt.Errorf("output.format %q is not valid; must be markdown or json", c.Output.Format)
	}
	return nil
}

// TimeRange resolves the configured timeframe to a concrete start and end time.
func (c *Config) TimeRange() (start, end time.Time, err error) {
	if c.Timeframe.LastDays > 0 {
		end = time.Now().UTC().Truncate(24 * time.Hour)
		start = end.AddDate(0, 0, -c.Timeframe.LastDays)
		return start, end, nil
	}

	start, err = time.Parse("2006-01-02", c.Timeframe.Start)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parsing timeframe.start: %w", err)
	}
	end, err = time.Parse("2006-01-02", c.Timeframe.End)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parsing timeframe.end: %w", err)
	}
	// End is inclusive — advance to end-of-day.
	end = end.Add(24*time.Hour - time.Second)
	return start, end, nil
}

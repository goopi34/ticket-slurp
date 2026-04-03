package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"slack-tickets/internal/config"
)

// writeConfig writes content to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

func validBase() string {
	return `
slack:
  xoxc: "xoxc-test"
  xoxd: "xoxd-test"
timeframe:
  start: "2026-03-01"
  end: "2026-04-01"
llm:
  provider: "azure"
  model: "gpt-4o"
  azure:
    endpoint: "https://example.azure.com"
    api_key: "key123"
    deployment: "gpt-4o"
atlassian:
  mcp_url: "http://localhost:3000"
  project_key: "ENG"
output:
  format: "markdown"
`
}

func TestLoad_Valid(t *testing.T) {
	path := writeConfig(t, validBase())
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestValidate_MissingXOXC(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxd: "xoxd-test"
timeframe:
  last_days: 7
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "k"
    deployment: "d"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing xoxc")
	}
}

func TestValidate_MissingXOXD(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "xoxc-test"
timeframe:
  last_days: 7
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "k"
    deployment: "d"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing xoxd")
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  last_days: 7
llm:
  provider: "badprovider"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid provider")
	}
}

func TestValidate_TimeframeMutuallyExclusive(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  start: "2026-01-01"
  end: "2026-02-01"
  last_days: 7
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "k"
    deployment: "d"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for mutually exclusive timeframe settings")
	}
}

func TestValidate_EndBeforeStart(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  start: "2026-04-01"
  end: "2026-03-01"
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "k"
    deployment: "d"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for end before start")
	}
}

func TestValidate_InvalidOutputFormat(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  last_days: 7
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "k"
    deployment: "d"
atlassian:
  project_key: "ENG"
output:
  format: "xml"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid output format")
	}
}

func TestValidate_MissingProjectKey(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  last_days: 7
llm:
  provider: "ollama"
  ollama:
    endpoint: "http://localhost:11434"
atlassian:
  mcp_url: "http://localhost:3000"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for missing project_key")
	}
}

func TestEnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_AZURE_KEY", "expanded-api-key")
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  last_days: 7
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "${TEST_AZURE_KEY}"
    deployment: "d"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.LLM.Azure.APIKey != "expanded-api-key" {
		t.Errorf("env var expansion failed: got %q, want %q", cfg.LLM.Azure.APIKey, "expanded-api-key")
	}
}

func TestTimeRange_LastDays(t *testing.T) {
	path := writeConfig(t, `
slack:
  xoxc: "x"
  xoxd: "y"
timeframe:
  last_days: 7
llm:
  provider: "azure"
  azure:
    endpoint: "https://x"
    api_key: "k"
    deployment: "d"
atlassian:
  project_key: "ENG"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	start, end, err := cfg.TimeRange()
	if err != nil {
		t.Fatalf("TimeRange(): %v", err)
	}
	diff := end.Sub(start)
	days := int(diff.Hours() / 24)
	if days != 7 {
		t.Errorf("expected 7 days range, got %d days", days)
	}
}

func TestTimeRange_ExplicitDates(t *testing.T) {
	path := writeConfig(t, validBase())
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	start, end, err := cfg.TimeRange()
	if err != nil {
		t.Fatalf("TimeRange(): %v", err)
	}
	expectedStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(expectedStart) {
		t.Errorf("start: got %v, want %v", start, expectedStart)
	}
	// End should be 2026-04-01 23:59:59 UTC
	if end.Before(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("end should be at or after 2026-04-01, got %v", end)
	}
}

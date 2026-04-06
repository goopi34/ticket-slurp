package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AzureGenerator implements LLMGenerator by calling the Azure OpenAI
// chat completions API directly, bypassing gollm which does not correctly
// propagate the azure_endpoint extra header to its internal HTTP client.
type AzureGenerator struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

// NewAzureGenerator creates an AzureGenerator.
// endpoint must be the full Azure OpenAI chat completions URL, e.g.
// https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version=2024-02-01
func NewAzureGenerator(endpoint, apiKey string) *AzureGenerator {
	return &AzureGenerator{
		endpoint: endpoint,
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

// Generate implements LLMGenerator.
func (g *AzureGenerator) Generate(ctx context.Context, prompt, systemPrompt string) (string, error) {
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": prompt},
	}

	body, err := json.Marshal(map[string]interface{}{"messages": messages})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", g.apiKey)

	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("azure OpenAI HTTP %s: %s", resp.Status, respBody)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("azure OpenAI error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("azure OpenAI returned no choices")
	}

	return result.Choices[0].Message.Content, nil
}

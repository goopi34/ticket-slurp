package atlassian

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"slack-tickets/internal/analysis"
)

// MCPClient implements Client by speaking the Model Context Protocol over HTTP/SSE
// to an Atlassian MCP server (e.g. mcp-atlassian running in Docker).
type MCPClient struct {
	baseURL    string
	projectKey string
	http       *http.Client
	idCounter  atomic.Int64
}

// NewMCPClient creates a new MCPClient.
func NewMCPClient(mcpURL, projectKey string, httpClient *http.Client) *MCPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &MCPClient{
		baseURL:    strings.TrimRight(mcpURL, "/"),
		projectKey: projectKey,
		http:       httpClient,
	}
}

// FindExisting implements Client.
func (c *MCPClient) FindExisting(ctx context.Context, candidates []analysis.TicketCandidate) (map[string]string, error) {
	result := make(map[string]string)

	for _, candidate := range candidates {
		key, found, err := c.searchForCandidate(ctx, candidate)
		if err != nil {
			// Log and continue; a single failure shouldn't abort everything.
			// In production this would be logged; here we skip the candidate.
			continue
		}
		if found {
			result[candidate.ID] = key
		}
	}

	return result, nil
}

// searchForCandidate looks for an existing Jira issue matching the candidate title.
func (c *MCPClient) searchForCandidate(ctx context.Context, candidate analysis.TicketCandidate) (key string, found bool, err error) {
	// Build a JQL query that searches by summary text within the configured project.
	jql := fmt.Sprintf(`project = "%s" AND summary ~ "%s" ORDER BY created DESC`,
		c.projectKey, escapeJQL(candidate.Title))

	resp, err := c.callTool(ctx, "jira_search", map[string]interface{}{
		"jql":        jql,
		"max_results": 1,
	})
	if err != nil {
		return "", false, fmt.Errorf("MCP tool call for candidate %q: %w", candidate.ID, err)
	}

	return parseSearchResult(resp)
}

// --- MCP protocol types ---

type mcpRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type mcpToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// callTool invokes the named MCP tool and returns the text content of the response.
func (c *MCPClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	id := c.idCounter.Add(1)
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params: mcpToolCallParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal MCP request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("HTTP request to MCP server: %w", err)
	}
	defer httpResp.Body.Close() //nolint:errcheck // best-effort close

	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MCP server HTTP %s", httpResp.Status)
	}

	contentType := httpResp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return c.readSSEResponse(httpResp.Body, id)
	}

	return c.readJSONResponse(httpResp.Body)
}

// readJSONResponse reads a plain JSON MCP response.
func (c *MCPClient) readJSONResponse(body io.Reader) (string, error) {
	var resp mcpResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return "", fmt.Errorf("decode MCP JSON response: %w", err)
	}
	return extractText(&resp)
}

// readSSEResponse reads a Server-Sent Events stream and extracts the MCP response matching id.
func (c *MCPClient) readSSEResponse(body io.Reader, id int64) (string, error) {
	scanner := bufio.NewScanner(body)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "" && len(dataLines) > 0 {
			// End of SSE event — try to parse.
			data := strings.Join(dataLines, "")
			dataLines = nil

			var resp mcpResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				continue // not an MCP response; skip
			}
			if resp.ID == id {
				return extractText(&resp)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading SSE stream: %w", err)
	}
	return "", fmt.Errorf("MCP response with id %d not found in SSE stream", id)
}

// extractText pulls the text content out of an MCP response.
func extractText(resp *mcpResponse) (string, error) {
	if resp.Error != nil {
		return "", fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result mcpToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// Maybe it's directly a text result; return raw JSON.
		return string(resp.Result), nil
	}

	if result.IsError {
		return "", fmt.Errorf("MCP tool returned error result")
	}

	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}
	return strings.Join(texts, "\n"), nil
}

// parseSearchResult inspects the Jira search result text for an issue key.
func parseSearchResult(text string) (key string, found bool, err error) {
	// The mcp-atlassian server returns results in various formats.
	// We try JSON first (typical structured output), then text scan.
	text = strings.TrimSpace(text)

	// Try JSON array of issues.
	var issues []struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(text), &issues); err == nil {
		if len(issues) > 0 && issues[0].Key != "" {
			return issues[0].Key, true, nil
		}
		return "", false, nil
	}

	// Try JSON object with "issues" field.
	var wrapper struct {
		Issues []struct {
			Key string `json:"key"`
		} `json:"issues"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &wrapper); err == nil {
		if wrapper.Total > 0 && len(wrapper.Issues) > 0 && wrapper.Issues[0].Key != "" {
			return wrapper.Issues[0].Key, true, nil
		}
		return "", false, nil
	}

	// Fall back to a simple text scan for a Jira-style key (e.g. ENG-123).
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, `"',.:;`)
		if isJiraKey(word) {
			return word, true, nil
		}
	}

	return "", false, nil
}

// isJiraKey returns true if the string looks like a Jira issue key (e.g. "ENG-123").
func isJiraKey(s string) bool {
	if len(s) < 3 {
		return false
	}
	dashIdx := strings.Index(s, "-")
	if dashIdx < 1 {
		return false
	}
	prefix := s[:dashIdx]
	suffix := s[dashIdx+1:]
	if len(suffix) == 0 {
		return false
	}
	for _, c := range prefix {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// escapeJQL escapes a string for use in a JQL ~ (text search) clause.
func escapeJQL(s string) string {
	// Remove or escape characters that have special meaning in JQL text search.
	s = strings.ReplaceAll(s, `"`, `\"`)
	// Limit to first 100 chars to avoid overly-long queries.
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

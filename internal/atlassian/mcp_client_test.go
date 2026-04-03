package atlassian_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"slack-tickets/internal/analysis"
	"slack-tickets/internal/atlassian"
)

func makeCandidate(id, title string) analysis.TicketCandidate {
	return analysis.TicketCandidate{
		ID:    id,
		Title: title,
	}
}

// mcpToolCallResponse builds a well-formed MCP JSON response for a tools/call.
func mcpToolCallResponse(id int64, textContent string) interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": textContent},
			},
			"isError": false,
		},
	}
}

func mcpErrorResponse(id int64, code int, msg string) interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]interface{}{"code": code, "message": msg},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("writeJSON: %v", err)
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	data, err := json.Marshal(v)
	if err != nil {
		t.Errorf("marshal SSE data: %v", err)
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		t.Errorf("write SSE: %v", err)
	}
}

// --- tests ---

func TestFindExisting_NoMatch_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id := int64(req["id"].(float64))
		// Return an empty issues list.
		writeJSON(t, w, mcpToolCallResponse(id, `{"issues":[],"total":0}`))
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{makeCandidate("C001_ts1", "Fix login bug")}

	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("expected 0 matches, got %v", found)
	}
}

func TestFindExisting_MatchFound_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id := int64(req["id"].(float64))
		writeJSON(t, w, mcpToolCallResponse(id, `{"issues":[{"key":"ENG-42"}],"total":1}`))
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{makeCandidate("C001_ts1", "Fix login bug")}

	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting: %v", err)
	}
	if found["C001_ts1"] != "ENG-42" {
		t.Errorf("expected ENG-42 match, got %v", found)
	}
}

func TestFindExisting_MatchFound_SSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id := int64(req["id"].(float64))
		writeSSE(t, w, mcpToolCallResponse(id, `{"issues":[{"key":"ENG-99"}],"total":1}`))
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{makeCandidate("C001_ts1", "Fix login bug")}

	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting: %v", err)
	}
	if found["C001_ts1"] != "ENG-99" {
		t.Errorf("expected ENG-99 match, got %v", found)
	}
}

func TestFindExisting_MCPError_Skips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id := int64(req["id"].(float64))
		writeJSON(t, w, mcpErrorResponse(id, -32603, "internal error"))
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{makeCandidate("C001_ts1", "Fix login bug")}

	// MCP error should not propagate as a Go error; candidate is simply skipped.
	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting should not error on MCP tool error, got: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("expected no matches on error, got %v", found)
	}
}

func TestFindExisting_MultipleCandidates(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id := int64(req["id"].(float64))
		// First call: match; second call: no match.
		if callCount == 1 {
			writeJSON(t, w, mcpToolCallResponse(id, `{"issues":[{"key":"ENG-1"}],"total":1}`))
		} else {
			writeJSON(t, w, mcpToolCallResponse(id, `{"issues":[],"total":0}`))
		}
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{
		makeCandidate("C001_ts1", "First issue"),
		makeCandidate("C001_ts2", "Second issue"),
	}

	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting: %v", err)
	}
	if found["C001_ts1"] != "ENG-1" {
		t.Errorf("expected ENG-1 for first candidate, got %v", found)
	}
	if _, ok := found["C001_ts2"]; ok {
		t.Errorf("second candidate should have no match")
	}
	if callCount != 2 {
		t.Errorf("expected 2 MCP calls, got %d", callCount)
	}
}

func TestFindExisting_TextFallback(t *testing.T) {
	// The server returns a plain-text response mentioning a Jira key.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id := int64(req["id"].(float64))
		writeJSON(t, w, mcpToolCallResponse(id, "Found issue ENG-77 matching your query."))
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{makeCandidate("C001_ts1", "Fix login bug")}

	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting: %v", err)
	}
	if found["C001_ts1"] != "ENG-77" {
		t.Errorf("expected ENG-77 from text fallback, got %v", found)
	}
}

func TestFindExisting_HTTPError_Skips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
	candidates := []analysis.TicketCandidate{makeCandidate("C001_ts1", "Fix login bug")}

	found, err := client.FindExisting(context.Background(), candidates)
	if err != nil {
		t.Fatalf("FindExisting should not propagate HTTP errors: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("expected no matches on HTTP error, got %v", found)
	}
}

func TestIsJiraKey(t *testing.T) {
	// Test through FindExisting's text-scan path.
	tests := []struct {
		text     string
		wantKey  string
		wantFind bool
	}{
		{"ENG-123", "ENG-123", true},
		{"PROJ-1", "PROJ-1", true},
		{"not-a-key", "", false},
		{"eng-123", "", false},   // lowercase prefix
		{"ENG-12X", "", false},   // non-numeric suffix
		{"ENG-", "", false},      // empty suffix
		{"-123", "", false},      // empty prefix
		{"", "", false},
	}

	for _, tt := range tests {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			id := int64(req["id"].(float64))
			writeJSON(t, w, mcpToolCallResponse(id, tt.text))
		}))

		client := atlassian.NewMCPClient(srv.URL, "ENG", nil)
		candidates := []analysis.TicketCandidate{makeCandidate("cid", "Some title")}
		found, _ := client.FindExisting(context.Background(), candidates)

		if tt.wantFind {
			if found["cid"] != tt.wantKey {
				t.Errorf("text=%q: expected key %q, got %q", tt.text, tt.wantKey, found["cid"])
			}
		} else {
			if _, ok := found["cid"]; ok {
				t.Errorf("text=%q: expected no match, got %q", tt.text, found["cid"])
			}
		}

		srv.Close()

		_ = strings.TrimSpace(tt.text) // suppress unused warning
	}
}

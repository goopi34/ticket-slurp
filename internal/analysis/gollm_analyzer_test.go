package analysis_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ticket-slurp/internal/analysis"
	"ticket-slurp/internal/slack"
)

// mockGenerator is a test double for LLMGenerator.
type mockGenerator struct {
	response string
	err      error
	// recorded allows tests to inspect the prompt that was sent.
	recorded []string
}

func (m *mockGenerator) Generate(_ context.Context, prompt, _ string) (string, error) {
	m.recorded = append(m.recorded, prompt)
	return m.response, m.err
}

func makeConv(id, name string) slack.Conversation {
	return slack.Conversation{ID: id, Name: name, IsChannel: true}
}

func makeMsg(ts, text string) slack.Message {
	return slack.Message{
		TS:     ts,
		UserID: "U001",
		Text:   text,
		Time:   time.Unix(1711929600, 0).UTC(),
	}
}

func TestIdentifyTickets_ValidResponse(t *testing.T) {
	candidates := []map[string]interface{}{
		{
			"id":                  "C001_1711929600.000000",
			"title":               "Fix login timeout bug",
			"description":         "Users report being logged out unexpectedly after 5 minutes.",
			"priority":            "high",
			"source_channel":      "C001",
			"source_channel_name": "eng-bugs",
			"source_message_ids":  []string{"1711929600.000000"},
			"rationale":           "Multiple users reported a reproducible bug requiring a code fix.",
		},
	}
	payload, _ := json.Marshal(candidates)

	gen := &mockGenerator{response: string(payload)}
	analyzer := analysis.NewGollmAnalyzer(gen)

	conv := makeConv("C001", "eng-bugs")
	msgs := []slack.Message{makeMsg("1711929600.000000", "We keep getting logged out after 5 minutes!")}

	result, err := analyzer.IdentifyTickets(context.Background(), conv, msgs)
	if err != nil {
		t.Fatalf("IdentifyTickets: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(result))
	}
	c := result[0]
	if c.Title != "Fix login timeout bug" {
		t.Errorf("title: got %q", c.Title)
	}
	if c.Priority != analysis.PriorityHigh {
		t.Errorf("priority: got %q, want high", c.Priority)
	}
	// Source channel must be overridden from conversation, not trusted from LLM.
	if c.SourceChannel != "C001" {
		t.Errorf("source_channel: got %q, want C001", c.SourceChannel)
	}
	if c.SourceChannelName != "eng-bugs" {
		t.Errorf("source_channel_name: got %q", c.SourceChannelName)
	}
}

func TestIdentifyTickets_EmptyMessages(t *testing.T) {
	gen := &mockGenerator{}
	analyzer := analysis.NewGollmAnalyzer(gen)

	result, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "test"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for empty messages, got %v", result)
	}
	if len(gen.recorded) != 0 {
		t.Error("LLM should not be called when there are no messages")
	}
}

func TestIdentifyTickets_EmptyArray(t *testing.T) {
	gen := &mockGenerator{response: "[]"}
	analyzer := analysis.NewGollmAnalyzer(gen)

	result, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "test"),
		[]slack.Message{makeMsg("1711929600.000000", "all good")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result))
	}
}

func TestIdentifyTickets_LLMError(t *testing.T) {
	gen := &mockGenerator{err: context.DeadlineExceeded}
	analyzer := analysis.NewGollmAnalyzer(gen)

	_, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "test"),
		[]slack.Message{makeMsg("1711929600.000000", "error test")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIdentifyTickets_InvalidJSON(t *testing.T) {
	gen := &mockGenerator{response: "not json at all"}
	analyzer := analysis.NewGollmAnalyzer(gen)

	_, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "test"),
		[]slack.Message{makeMsg("1711929600.000000", "msg")})
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestIdentifyTickets_CodeFenceStripped(t *testing.T) {
	raw := "```json\n[]\n```"
	gen := &mockGenerator{response: raw}
	analyzer := analysis.NewGollmAnalyzer(gen)

	result, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "test"),
		[]slack.Message{makeMsg("1711929600.000000", "msg")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result))
	}
}

func TestIdentifyTickets_PriorityNormalisation(t *testing.T) {
	tests := []struct {
		input    string
		expected analysis.Priority
	}{
		{"high", analysis.PriorityHigh},
		{"HIGH", analysis.PriorityHigh},
		{"medium", analysis.PriorityMedium},
		{"low", analysis.PriorityLow},
		{"unknown", analysis.PriorityMedium}, // default
		{"", analysis.PriorityMedium},
	}

	for _, tt := range tests {
		candidates := []map[string]interface{}{
			{"title": "t", "priority": tt.input, "source_channel": "C001"},
		}
		payload, _ := json.Marshal(candidates)
		gen := &mockGenerator{response: string(payload)}
		analyzer := analysis.NewGollmAnalyzer(gen)

		result, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "ch"),
			[]slack.Message{makeMsg("1711929600.000000", "msg")})
		if err != nil {
			t.Fatalf("priority %q: unexpected error: %v", tt.input, err)
		}
		if len(result) != 1 {
			t.Fatalf("priority %q: expected 1 result, got %d", tt.input, len(result))
		}
		if result[0].Priority != tt.expected {
			t.Errorf("priority %q: got %q, want %q", tt.input, result[0].Priority, tt.expected)
		}
	}
}

func TestBuildPrompt_ContainsChannelAndMessages(t *testing.T) {
	var capturedPrompt string
	gen := &mockGenerator{}
	gen.response = "[]"

	// We exercise the prompt via IdentifyTickets; inspect what was recorded.
	analyzer := analysis.NewGollmAnalyzer(gen)
	conv := makeConv("C001", "eng-general")
	msgs := []slack.Message{makeMsg("1711929600.000000", "hello world")}

	if _, err := analyzer.IdentifyTickets(context.Background(), conv, msgs); err != nil {
		t.Fatalf("IdentifyTickets: %v", err)
	}

	if len(gen.recorded) == 0 {
		t.Fatal("no prompt recorded")
	}
	capturedPrompt = gen.recorded[0]

	if !strings.Contains(capturedPrompt, "eng-general") {
		t.Error("prompt should contain the channel name")
	}
	if !strings.Contains(capturedPrompt, "hello world") {
		t.Error("prompt should contain the message text")
	}
	if !strings.Contains(capturedPrompt, "C001") {
		t.Error("prompt should contain the channel ID")
	}
}

func TestBuildPrompt_LongMessageTruncated(t *testing.T) {
	gen := &mockGenerator{response: "[]"}
	analyzer := analysis.NewGollmAnalyzer(gen)

	// Create a message that exceeds the 2000-char truncation threshold.
	longText := strings.Repeat("a", 2500)
	msgs := []slack.Message{makeMsg("1711929600.000000", longText)}

	if _, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "ch"), msgs); err != nil {
		t.Fatalf("IdentifyTickets: %v", err)
	}

	if len(gen.recorded) == 0 {
		t.Fatal("no prompt recorded")
	}
	prompt := gen.recorded[0]
	if strings.Contains(prompt, longText) {
		t.Error("prompt should have the long message truncated")
	}
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("prompt should contain [truncated] marker")
	}
}

func TestIdentifyTickets_SkipsMalformedEntries(t *testing.T) {
	// Entries with no title should be silently dropped.
	payload := `[{"title":"","priority":"high"},{"title":"Valid title","priority":"low"}]`
	gen := &mockGenerator{response: payload}
	analyzer := analysis.NewGollmAnalyzer(gen)

	result, err := analyzer.IdentifyTickets(context.Background(), makeConv("C001", "ch"),
		[]slack.Message{makeMsg("ts1", "msg")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 valid candidate, got %d", len(result))
	}
	if result[0].Title != "Valid title" {
		t.Errorf("unexpected title: %q", result[0].Title)
	}
}

package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ticket-slurp/internal/slack"
)

const systemPrompt = `You are an engineering team assistant that reviews Slack conversation history
to identify work that should be tracked as engineering tickets.

Your task is to analyze the messages provided and extract actionable work items such as:
- Bug reports or production issues that need investigation
- Feature requests that have been agreed upon
- Technical debt items that have been acknowledged
- Blocked work items that need unblocking
- Infrastructure or tooling improvements discussed
- Security concerns requiring remediation

For each work item, you must output a JSON array with objects matching this schema:
{
  "id": "<channel_id>_<first_message_ts>",
  "title": "<concise one-line title, max 80 chars>",
  "description": "<detailed description suitable for a Jira ticket body>",
  "priority": "<high|medium|low>",
  "source_channel": "<channel_id>",
  "source_channel_name": "<channel_name>",
  "source_message_ids": ["<ts1>", "<ts2>", ...],
  "rationale": "<brief explanation of why this warrants a ticket>"
}

Rules:
- Only include items that represent concrete actionable work
- Exclude casual conversation, already-resolved topics, and purely informational messages
- Respond ONLY with the JSON array — no prose, no markdown code fences, no commentary
- If no ticket-worthy items are found, respond with an empty JSON array: []`

// GollmAnalyzer implements Analyzer using an LLMGenerator.
type GollmAnalyzer struct {
	gen          LLMGenerator
	systemPrompt string
}

// NewGollmAnalyzer creates a new GollmAnalyzer with the provided LLMGenerator.
// If customPrompt is non-empty it replaces the built-in system prompt.
func NewGollmAnalyzer(gen LLMGenerator, customPrompt string) *GollmAnalyzer {
	prompt := systemPrompt
	if customPrompt != "" {
		prompt = customPrompt
	}
	return &GollmAnalyzer{gen: gen, systemPrompt: prompt}
}

// IdentifyTickets implements Analyzer.
func (a *GollmAnalyzer) IdentifyTickets(ctx context.Context, conv slack.Conversation, msgs []slack.Message) ([]TicketCandidate, error) {
	if len(msgs) == 0 {
		return nil, nil
	}

	prompt := buildPrompt(conv, msgs)

	raw, err := a.gen.Generate(ctx, prompt, a.systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("generate for channel %s: %w", conv.ID, err)
	}

	candidates, err := parseResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse LLM response for channel %s: %w", conv.ID, err)
	}

	// Ensure source channel fields are correctly set (don't trust LLM for these).
	for i := range candidates {
		candidates[i].SourceChannel = conv.ID
		candidates[i].SourceChannelName = conv.Name
	}

	return candidates, nil
}

// buildPrompt constructs the user-facing prompt from the conversation and messages.
func buildPrompt(conv slack.Conversation, msgs []slack.Message) string {
	var sb strings.Builder

	channelDesc := conv.Name
	if channelDesc == "" {
		channelDesc = conv.ID
	}

	fmt.Fprintf(&sb, "Channel: %s (ID: %s)\n\n", channelDesc, conv.ID)
	fmt.Fprintf(&sb, "Messages (%d total):\n", len(msgs))

	for _, m := range msgs {
		ts := m.Time.Format(time.RFC3339)
		// Truncate very long messages to avoid token limits.
		text := m.Text
		if len(text) > 2000 {
			text = text[:2000] + "... [truncated]"
		}
		fmt.Fprintf(&sb, "[%s] user=%s ts=%s: %s\n", ts, m.UserID, m.TS, text)
	}

	return sb.String()
}

// candidateJSON mirrors TicketCandidate for JSON unmarshalling.
type candidateJSON struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Priority          string   `json:"priority"`
	SourceChannel     string   `json:"source_channel"`
	SourceChannelName string   `json:"source_channel_name"`
	SourceMessageIDs  []string `json:"source_message_ids"`
	Rationale         string   `json:"rationale"`
}

// parseResponse parses the raw LLM output into TicketCandidates.
// It is tolerant of leading/trailing whitespace and strips accidental code fences.
func parseResponse(raw string) ([]TicketCandidate, error) {
	raw = strings.TrimSpace(raw)

	// Strip markdown code fences if the LLM wrapped its output anyway.
	if strings.HasPrefix(raw, "```") {
		raw = stripCodeFence(raw)
	}

	var items []candidateJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w (raw: %q)", err, truncate(raw, 200))
	}

	out := make([]TicketCandidate, 0, len(items))
	for _, item := range items {
		if item.Title == "" {
			continue // skip malformed entries
		}
		out = append(out, TicketCandidate{
			ID:                item.ID,
			Title:             item.Title,
			Description:       item.Description,
			Priority:          normalisePriority(item.Priority),
			SourceChannel:     item.SourceChannel,
			SourceChannelName: item.SourceChannelName,
			SourceMessageIDs:  item.SourceMessageIDs,
			Rationale:         item.Rationale,
		})
	}
	return out, nil
}

func normalisePriority(s string) Priority {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return PriorityHigh
	case "low":
		return PriorityLow
	default:
		return PriorityMedium
	}
}

func stripCodeFence(s string) string {
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) < 2 {
		return s
	}
	body := lines[1]
	if idx := strings.LastIndex(body, "```"); idx >= 0 {
		body = body[:idx]
	}
	return strings.TrimSpace(body)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

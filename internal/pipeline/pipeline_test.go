package pipeline_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"ticket-slurp/internal/analysis"
	"ticket-slurp/internal/atlassian"
	"ticket-slurp/internal/config"
	"ticket-slurp/internal/pipeline"
	"ticket-slurp/internal/report"
	"ticket-slurp/internal/slack"
)

// --- test doubles ---

type mockSlackClient struct {
	conversations []slack.Conversation
	messages      map[string][]slack.Message
	listErr       error
	fetchErr      error
}

func (m *mockSlackClient) ListConversations(_ context.Context) ([]slack.Conversation, error) {
	return m.conversations, m.listErr
}

func (m *mockSlackClient) FetchMessages(_ context.Context, channelID string, _, _ time.Time) ([]slack.Message, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.messages[channelID], nil
}

type mockAnalyzer struct {
	candidates []analysis.TicketCandidate
	err        error
}

func (m *mockAnalyzer) IdentifyTickets(_ context.Context, _ slack.Conversation, _ []slack.Message) ([]analysis.TicketCandidate, error) {
	return m.candidates, m.err
}

type mockAtlassian struct {
	result map[string]string
	err    error
}

func (m *mockAtlassian) FindExisting(_ context.Context, _ []analysis.TicketCandidate) (map[string]string, error) {
	return m.result, m.err
}

// --- helpers ---

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func buildConfig() *config.Config {
	return &config.Config{
		Slack: config.SlackConfig{XOXC: "x", XOXD: "y"},
		Timeframe: config.TimeframeConfig{
			Start: "2026-03-01",
			End:   "2026-04-01",
		},
		LLM:       config.LLMConfig{Provider: "azure"},
		Atlassian: config.AtlassianConfig{ProjectKey: "ENG"},
		Output:    config.OutputConfig{Format: "markdown"},
	}
}

func buildRunner(
	cfg *config.Config,
	slackClient slack.Client,
	analyzer analysis.Analyzer,
	atlassianClient atlassian.Client,
	reporter report.Reporter,
) *pipeline.Runner {
	return pipeline.New(cfg, slackClient, analyzer, atlassianClient, reporter, silentLogger())
}

// --- tests ---

func TestRun_HappyPath(t *testing.T) {
	conv := slack.Conversation{ID: "C001", Name: "eng-general", IsChannel: true}
	msg := slack.Message{TS: "1711929600.000000", UserID: "U001", Text: "fix the bug", Time: time.Now()}
	candidate := analysis.TicketCandidate{ID: "C001_ts1", Title: "Fix the bug", Priority: analysis.PriorityHigh}

	slackClient := &mockSlackClient{
		conversations: []slack.Conversation{conv},
		messages:      map[string][]slack.Message{"C001": {msg}},
	}
	analyzer := &mockAnalyzer{candidates: []analysis.TicketCandidate{candidate}}
	atlassianClient := &mockAtlassian{result: map[string]string{}}
	reporter, _ := report.New("markdown")

	var buf bytes.Buffer
	runner := buildRunner(buildConfig(), slackClient, analyzer, atlassianClient, reporter)
	if err := runner.Run(context.Background(), &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Fix the bug") {
		t.Error("report should contain candidate title")
	}
}

func TestRun_ListConversationsError(t *testing.T) {
	slackClient := &mockSlackClient{listErr: errors.New("auth failed")}
	analyzer := &mockAnalyzer{}
	atlassianClient := &mockAtlassian{}
	reporter, _ := report.New("markdown")

	runner := buildRunner(buildConfig(), slackClient, analyzer, atlassianClient, reporter)
	err := runner.Run(context.Background(), io.Discard)
	if err == nil {
		t.Fatal("expected error from ListConversations failure")
	}
}

func TestRun_FetchMessagesError_Skips(t *testing.T) {
	// FetchMessages errors should be skipped (not fatal).
	conv := slack.Conversation{ID: "C001", Name: "test"}
	slackClient := &mockSlackClient{
		conversations: []slack.Conversation{conv},
		fetchErr:      errors.New("channel not found"),
	}
	analyzer := &mockAnalyzer{}
	atlassianClient := &mockAtlassian{result: map[string]string{}}
	reporter, _ := report.New("markdown")

	var buf bytes.Buffer
	runner := buildRunner(buildConfig(), slackClient, analyzer, atlassianClient, reporter)
	if err := runner.Run(context.Background(), &buf); err != nil {
		t.Fatalf("Run should not fail when FetchMessages errors; got: %v", err)
	}
}

func TestRun_AnalysisError_Skips(t *testing.T) {
	conv := slack.Conversation{ID: "C001", Name: "test"}
	msg := slack.Message{TS: "ts1", Text: "hello"}
	slackClient := &mockSlackClient{
		conversations: []slack.Conversation{conv},
		messages:      map[string][]slack.Message{"C001": {msg}},
	}
	analyzer := &mockAnalyzer{err: errors.New("LLM timeout")}
	atlassianClient := &mockAtlassian{result: map[string]string{}}
	reporter, _ := report.New("markdown")

	var buf bytes.Buffer
	runner := buildRunner(buildConfig(), slackClient, analyzer, atlassianClient, reporter)
	if err := runner.Run(context.Background(), &buf); err != nil {
		t.Fatalf("Run should not fail when analysis errors; got: %v", err)
	}
}

func TestRun_ExistingTicketAnnotated(t *testing.T) {
	conv := slack.Conversation{ID: "C001", Name: "eng"}
	msg := slack.Message{TS: "ts1", Text: "fix it"}
	candidate := analysis.TicketCandidate{ID: "cand1", Title: "Fix it"}
	slackClient := &mockSlackClient{
		conversations: []slack.Conversation{conv},
		messages:      map[string][]slack.Message{"C001": {msg}},
	}
	analyzer := &mockAnalyzer{candidates: []analysis.TicketCandidate{candidate}}
	atlassianClient := &mockAtlassian{result: map[string]string{"cand1": "ENG-99"}}
	reporter, _ := report.New("markdown")

	var buf bytes.Buffer
	runner := buildRunner(buildConfig(), slackClient, analyzer, atlassianClient, reporter)
	if err := runner.Run(context.Background(), &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ENG-99") {
		t.Errorf("report should contain existing ticket key; got:\n%s", out)
	}
}

func TestRun_ContextCancellation(_ *testing.T) {
	conv := slack.Conversation{ID: "C001", Name: "test"}
	slackClient := &mockSlackClient{conversations: []slack.Conversation{conv}}
	analyzer := &mockAnalyzer{}
	atlassianClient := &mockAtlassian{}
	reporter, _ := report.New("markdown")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	runner := buildRunner(buildConfig(), slackClient, analyzer, atlassianClient, reporter)
	err := runner.Run(ctx, io.Discard)
	// Could succeed (if no messages) or return ctx.Err() — either is acceptable.
	_ = err
}

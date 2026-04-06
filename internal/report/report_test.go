package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ticket-slurp/internal/analysis"
	"ticket-slurp/internal/report"
)

func makeCandidate(id, title string, priority analysis.Priority, existingKey string) analysis.TicketCandidate {
	return analysis.TicketCandidate{
		ID:                id,
		Title:             title,
		Description:       "Description for " + title,
		Priority:          priority,
		SourceChannel:     "C001",
		SourceChannelName: "eng-general",
		SourceMessageIDs:  []string{"ts1", "ts2"},
		Rationale:         "This should be tracked.",
		ExistingTicketKey: existingKey,
	}
}

// --- New factory tests ---

func TestNew_ValidFormats(t *testing.T) {
	for _, format := range []string{"markdown", "md", "json", "Markdown", "JSON"} {
		r, err := report.New(format)
		if err != nil {
			t.Errorf("New(%q) unexpected error: %v", format, err)
		}
		if r == nil {
			t.Errorf("New(%q) returned nil reporter", format)
		}
	}
}

func TestNew_InvalidFormat(t *testing.T) {
	_, err := report.New("xml")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// --- MarkdownReporter tests ---

func TestMarkdownReporter_EmptyCandidates(t *testing.T) {
	r, _ := report.New("markdown")
	var buf bytes.Buffer
	if err := r.Write(&buf, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# Slack Ticket Candidates") {
		t.Error("markdown output should contain the main header")
	}
	if !strings.Contains(out, "0 candidate(s)") {
		t.Errorf("should report 0 candidates; got:\n%s", out)
	}
}

func TestMarkdownReporter_UntrackedCandidates(t *testing.T) {
	candidates := []analysis.TicketCandidate{
		makeCandidate("id1", "Fix login bug", analysis.PriorityHigh, ""),
		makeCandidate("id2", "Add dark mode", analysis.PriorityLow, ""),
	}

	r, _ := report.New("markdown")
	var buf bytes.Buffer
	if err := r.Write(&buf, candidates); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Fix login bug") {
		t.Error("should contain first candidate title")
	}
	if !strings.Contains(out, "Add dark mode") {
		t.Error("should contain second candidate title")
	}
	if !strings.Contains(out, "high") {
		t.Error("should contain priority 'high'")
	}
	if !strings.Contains(out, "2 candidate(s)") {
		t.Errorf("should report 2 candidates; got:\n%s", out)
	}
}

func TestMarkdownReporter_TrackedCandidates(t *testing.T) {
	candidates := []analysis.TicketCandidate{
		makeCandidate("id1", "Fix login bug", analysis.PriorityHigh, "ENG-42"),
	}

	r, _ := report.New("markdown")
	var buf bytes.Buffer
	if err := r.Write(&buf, candidates); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "ENG-42") {
		t.Error("should contain the existing ticket key")
	}
	if !strings.Contains(out, "Already Tracked") {
		t.Error("should contain 'Already Tracked' section")
	}
	if !strings.Contains(out, "0 candidate(s) without existing") {
		t.Errorf("should report 0 untracked; got:\n%s", out)
	}
}

func TestMarkdownReporter_MixedCandidates(t *testing.T) {
	candidates := []analysis.TicketCandidate{
		makeCandidate("id1", "Fix login bug", analysis.PriorityHigh, ""),     // untracked
		makeCandidate("id2", "Add dark mode", analysis.PriorityLow, "ENG-5"), // tracked
	}

	r, _ := report.New("markdown")
	var buf bytes.Buffer
	if err := r.Write(&buf, candidates); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "1 candidate(s) without existing") {
		t.Errorf("should report 1 untracked; got:\n%s", out)
	}
	if !strings.Contains(out, "Fix login bug") {
		t.Error("should contain the untracked candidate")
	}
	if !strings.Contains(out, "ENG-5") {
		t.Error("should contain the tracked ticket key")
	}
}

func TestMarkdownReporter_PipesEscaped(t *testing.T) {
	candidates := []analysis.TicketCandidate{
		makeCandidate("id1", "Fix A|B issue", analysis.PriorityMedium, ""),
	}

	r, _ := report.New("markdown")
	var buf bytes.Buffer
	if err := r.Write(&buf, candidates); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	// The pipe in the title should be escaped so it doesn't break the table.
	if strings.Contains(out, "Fix A|B") && !strings.Contains(out, `Fix A\|B`) {
		t.Error("pipe character in title should be escaped in markdown table")
	}
}

// --- JSONReporter tests ---

func TestJSONReporter_ValidOutput(t *testing.T) {
	candidates := []analysis.TicketCandidate{
		makeCandidate("id1", "Fix login bug", analysis.PriorityHigh, ""),
		makeCandidate("id2", "Add dark mode", analysis.PriorityLow, "ENG-5"),
	}

	r, _ := report.New("json")
	var buf bytes.Buffer
	if err := r.Write(&buf, candidates); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var out struct {
		GeneratedAt string                     `json:"generated_at"`
		Total       int                        `json:"total_identified"`
		NeedTickets int                        `json:"need_tickets"`
		Candidates  []analysis.TicketCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal JSON output: %v", err)
	}

	if out.Total != 2 {
		t.Errorf("total_identified: got %d, want 2", out.Total)
	}
	if out.NeedTickets != 1 {
		t.Errorf("need_tickets: got %d, want 1", out.NeedTickets)
	}
	if len(out.Candidates) != 2 {
		t.Errorf("candidates: got %d, want 2", len(out.Candidates))
	}
	if out.GeneratedAt == "" {
		t.Error("generated_at should be set")
	}
}

func TestJSONReporter_EmptyCandidates(t *testing.T) {
	r, _ := report.New("json")
	var buf bytes.Buffer
	if err := r.Write(&buf, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["total_identified"].(float64) != 0 {
		t.Error("expected total_identified = 0")
	}
}

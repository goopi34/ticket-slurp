// Package report renders ticket candidate reports in various formats.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"ticket-slurp/internal/analysis"
)

// Reporter writes a ticket candidate report to an io.Writer.
type Reporter interface {
	Write(w io.Writer, candidates []analysis.TicketCandidate) error
}

// New returns the appropriate Reporter for the configured format.
// format must be "markdown" or "json" (case-insensitive).
func New(format string) (Reporter, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		return &MarkdownReporter{}, nil
	case "json":
		return &JSONReporter{}, nil
	default:
		return nil, fmt.Errorf("unsupported report format %q: must be markdown or json", format)
	}
}

// MarkdownReporter renders candidates as a Markdown report.
type MarkdownReporter struct{}

// Write implements Reporter.
func (r *MarkdownReporter) Write(w io.Writer, candidates []analysis.TicketCandidate) error {
	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")

	if _, err := fmt.Fprintf(w, "# Slack Ticket Candidates\n\n_Generated %s_\n\n", now); err != nil {
		return err
	}

	untracked := filterUntracked(candidates)
	tracked := filterTracked(candidates)

	if _, err := fmt.Fprintf(w, "**%d candidate(s) without existing tickets** (of %d identified)\n\n",
		len(untracked), len(candidates)); err != nil {
		return err
	}

	if len(untracked) == 0 {
		if _, err := fmt.Fprintln(w, "_All identified work items already have Jira tickets._"); err != nil {
			return err
		}
	} else {
		if err := writeMarkdownTable(w, untracked); err != nil {
			return err
		}
	}

	if len(tracked) > 0 {
		if _, err := fmt.Fprintf(w, "\n## Already Tracked (%d)\n\n", len(tracked)); err != nil {
			return err
		}
		for _, c := range tracked {
			if _, err := fmt.Fprintf(w, "- **%s** → %s (%s)\n", c.Title, c.ExistingTicketKey, c.SourceChannelName); err != nil {
				return err
			}
		}
	}

	return nil
}

func writeMarkdownTable(w io.Writer, candidates []analysis.TicketCandidate) error {
	header := "| # | Title | Priority | Channel | Rationale |\n|---|-------|----------|---------|----------|\n"
	if _, err := fmt.Fprint(w, header); err != nil {
		return err
	}

	for i, c := range candidates {
		title := escapeMD(c.Title)
		priority := escapeMD(string(c.Priority))
		channel := escapeMD(c.SourceChannelName)
		if channel == "" {
			channel = c.SourceChannel
		}
		rationale := escapeMD(truncate(c.Rationale, 120))

		line := fmt.Sprintf("| %d | %s | %s | %s | %s |\n", i+1, title, priority, channel, rationale)
		if _, err := fmt.Fprint(w, line); err != nil {
			return err
		}
	}

	// Detail blocks for each candidate.
	if _, err := fmt.Fprintln(w, "\n## Details"); err != nil {
		return err
	}
	for i, c := range candidates {
		if err := writeMarkdownDetail(w, i+1, c); err != nil {
			return err
		}
	}

	return nil
}

func writeMarkdownDetail(w io.Writer, n int, c analysis.TicketCandidate) error {
	_, err := fmt.Fprintf(w, "\n### %d. %s\n\n**Priority:** %s  \n**Channel:** %s (`%s`)  \n**Source messages:** %s\n\n%s\n\n**Rationale:** %s\n",
		n,
		c.Title,
		c.Priority,
		c.SourceChannelName,
		c.SourceChannel,
		strings.Join(c.SourceMessageIDs, ", "),
		c.Description,
		c.Rationale,
	)
	return err
}

// JSONReporter renders candidates as JSON.
type JSONReporter struct{}

// jsonReport is the top-level JSON output structure.
type jsonReport struct {
	GeneratedAt string                     `json:"generated_at"`
	Total       int                        `json:"total_identified"`
	NeedTickets int                        `json:"need_tickets"`
	Candidates  []analysis.TicketCandidate `json:"candidates"`
}

// Write implements Reporter.
func (r *JSONReporter) Write(w io.Writer, candidates []analysis.TicketCandidate) error {
	untracked := filterUntracked(candidates)

	report := jsonReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Total:       len(candidates),
		NeedTickets: len(untracked),
		Candidates:  candidates,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	return nil
}

// --- helpers ---

func filterUntracked(candidates []analysis.TicketCandidate) []analysis.TicketCandidate {
	var out []analysis.TicketCandidate
	for _, c := range candidates {
		if c.ExistingTicketKey == "" {
			out = append(out, c)
		}
	}
	return out
}

func filterTracked(candidates []analysis.TicketCandidate) []analysis.TicketCandidate {
	var out []analysis.TicketCandidate
	for _, c := range candidates {
		if c.ExistingTicketKey != "" {
			out = append(out, c)
		}
	}
	return out
}

func escapeMD(s string) string {
	// Escape pipe characters to avoid breaking Markdown tables.
	return strings.ReplaceAll(s, "|", `\|`)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

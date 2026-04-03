// Package pipeline orchestrates the full slack-tickets workflow.
package pipeline

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"slack-tickets/internal/analysis"
	"slack-tickets/internal/atlassian"
	"slack-tickets/internal/config"
	"slack-tickets/internal/report"
	"slack-tickets/internal/slack"
)

// Runner executes the full pipeline.
type Runner struct {
	cfg      *config.Config
	slack    slack.Client
	analyzer analysis.Analyzer
	atlassian atlassian.Client
	reporter report.Reporter
	logger   *slog.Logger
}

// New creates a Runner with all dependencies injected.
func New(
	cfg *config.Config,
	slackClient slack.Client,
	analyzer analysis.Analyzer,
	atlassianClient atlassian.Client,
	reporter report.Reporter,
	logger *slog.Logger,
) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		cfg:      cfg,
		slack:    slackClient,
		analyzer: analyzer,
		atlassian: atlassianClient,
		reporter: reporter,
		logger:   logger,
	}
}

// Run executes the full pipeline and writes the report to w.
func (r *Runner) Run(ctx context.Context, w io.Writer) error {
	// 1. Resolve time range.
	from, to, err := r.cfg.TimeRange()
	if err != nil {
		return fmt.Errorf("resolve time range: %w", err)
	}
	r.logger.Info("time range resolved", "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"))

	// 2. List conversations.
	r.logger.Info("listing Slack conversations")
	conversations, err := r.slack.ListConversations(ctx)
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}
	r.logger.Info("conversations found", "count", len(conversations))

	// 3. Fetch messages and analyze each conversation.
	var allCandidates []analysis.TicketCandidate
	for _, conv := range conversations {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		r.logger.Info("fetching messages", "channel", conv.ID, "name", conv.Name)
		msgs, err := r.slack.FetchMessages(ctx, conv.ID, from, to)
		if err != nil {
			r.logger.Warn("failed to fetch messages; skipping", "channel", conv.ID, "err", err)
			continue
		}
		if len(msgs) == 0 {
			r.logger.Debug("no messages in range; skipping", "channel", conv.ID)
			continue
		}
		r.logger.Info("analyzing messages", "channel", conv.ID, "message_count", len(msgs))

		candidates, err := r.analyzer.IdentifyTickets(ctx, conv, msgs)
		if err != nil {
			r.logger.Warn("analysis failed; skipping", "channel", conv.ID, "err", err)
			continue
		}
		allCandidates = append(allCandidates, candidates...)
	}
	r.logger.Info("analysis complete", "total_candidates", len(allCandidates))

	// 4. Check existing tickets.
	if len(allCandidates) > 0 {
		r.logger.Info("checking existing Jira tickets", "candidate_count", len(allCandidates))
		existing, err := r.atlassian.FindExisting(ctx, allCandidates)
		if err != nil {
			return fmt.Errorf("check existing tickets: %w", err)
		}
		for i := range allCandidates {
			if key, ok := existing[allCandidates[i].ID]; ok {
				allCandidates[i].ExistingTicketKey = key
			}
		}
		r.logger.Info("existing ticket lookup complete", "matched", len(existing))
	}

	// 5. Write report.
	r.logger.Info("writing report")
	if err := r.reporter.Write(w, allCandidates); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	return nil
}

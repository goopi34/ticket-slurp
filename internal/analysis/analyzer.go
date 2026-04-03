package analysis

import (
	"context"

	"slack-tickets/internal/slack"
)

// Analyzer identifies potential tickets from Slack conversation messages.
type Analyzer interface {
	IdentifyTickets(ctx context.Context, conv slack.Conversation, msgs []slack.Message) ([]TicketCandidate, error)
}

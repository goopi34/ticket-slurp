package analysis

import (
	"context"

	"ticket-slurp/internal/slack"
)

// Analyzer identifies potential tickets from Slack conversation messages.
type Analyzer interface {
	IdentifyTickets(ctx context.Context, conv slack.Conversation, msgs []slack.Message) ([]TicketCandidate, error)
}

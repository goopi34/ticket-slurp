// Package analysis provides AI-powered identification of work that should become tickets.
package analysis

// Priority levels for ticket candidates.
type Priority string

const (
	// PriorityHigh indicates urgent work requiring immediate attention.
	PriorityHigh Priority = "high"
	// PriorityMedium indicates important work to be scheduled soon.
	PriorityMedium Priority = "medium"
	// PriorityLow indicates desirable work that can be deferred.
	PriorityLow Priority = "low"
)

// TicketCandidate is a piece of work identified from Slack messages that may warrant a ticket.
type TicketCandidate struct {
	// ID is a deterministic identifier derived from the source channel and first message TS.
	ID string `json:"id"`
	// Title is a concise one-line description of the work item.
	Title string `json:"title"`
	// Description is a more detailed description suitable for a ticket body.
	Description string `json:"description"`
	// Priority is the suggested ticket priority.
	Priority Priority `json:"priority"`
	// SourceChannel is the Slack channel ID this candidate originated from.
	SourceChannel string `json:"source_channel"`
	// SourceChannelName is the human-readable channel name.
	SourceChannelName string `json:"source_channel_name"`
	// SourceMessageIDs are the Slack message timestamps (TS fields) that support this candidate.
	SourceMessageIDs []string `json:"source_message_ids"`
	// Rationale explains why this was identified as a potential ticket.
	Rationale string `json:"rationale"`
	// ExistingTicketKey is set when an existing Jira ticket was found (empty = needs creation).
	ExistingTicketKey string `json:"existing_ticket_key,omitempty"`
}

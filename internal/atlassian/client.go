// Package atlassian provides a client for querying Jira via an MCP server.
package atlassian

import (
	"context"

	"ticket-slurp/internal/analysis"
)

// Client searches Jira for tickets that may already cover identified work.
type Client interface {
	// FindExisting returns a map from TicketCandidate.ID to the Jira issue key
	// for each candidate that has a matching existing ticket.
	// Candidates not found in Jira will not appear in the returned map.
	FindExisting(ctx context.Context, candidates []analysis.TicketCandidate) (map[string]string, error)
}

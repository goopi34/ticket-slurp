package slack

import (
	"context"
	"time"
)

// Client is the interface for interacting with Slack.
type Client interface {
	// ListConversations returns all conversations the authenticated user is a member of,
	// after applying whitelist/blacklist filtering.
	ListConversations(ctx context.Context) ([]Conversation, error)

	// FetchMessages returns all messages in the given conversation within [from, to].
	FetchMessages(ctx context.Context, channelID string, from, to time.Time) ([]Message, error)
}

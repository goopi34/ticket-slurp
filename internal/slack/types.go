// Package slack provides a client for Slack's API using xoxc/xoxd tokens.
package slack

import "time"

// Conversation represents a Slack channel, DM, or group DM.
type Conversation struct {
	ID         string
	Name       string // display name; empty for DMs
	IsChannel  bool
	IsIM       bool   // true for direct messages
	IsMPIM     bool   // true for group DMs
	IsPrivate  bool
	NumMembers int
}

// Message represents a single Slack message.
type Message struct {
	TS       string // Slack timestamp ID (e.g. "1711929600.123456")
	UserID   string
	Text     string
	Time     time.Time
	ThreadTS string // non-empty if this is a thread reply
}

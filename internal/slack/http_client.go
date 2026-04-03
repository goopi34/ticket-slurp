package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL    = "https://slack.com/api"
	conversationTypes = "public_channel,private_channel,mpim,im"
)

// HTTPClient implements Client using Slack's HTTP API with xoxc/xoxd credentials.
type HTTPClient struct {
	baseURL   string
	xoxc      string
	xoxd      string
	whitelist map[string]bool
	blacklist map[string]bool
	http      *http.Client
}

// NewHTTPClient creates a new HTTPClient.
// whitelist and blacklist are channel ID sets. If whitelist is non-empty,
// only those IDs are included (before blacklist is applied).
func NewHTTPClient(xoxc, xoxd string, whitelist, blacklist []string, httpClient *http.Client) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	wl := toSet(whitelist)
	bl := toSet(blacklist)
	return &HTTPClient{
		baseURL:   defaultBaseURL,
		xoxc:      xoxc,
		xoxd:      xoxd,
		whitelist: wl,
		blacklist: bl,
		http:      httpClient,
	}
}

// WithBaseURL overrides the API base URL (used in tests).
func (c *HTTPClient) WithBaseURL(u string) *HTTPClient {
	c.baseURL = u
	return c
}

// ListConversations implements Client.
func (c *HTTPClient) ListConversations(ctx context.Context) ([]Conversation, error) {
	var all []Conversation
	cursor := ""

	for {
		params := url.Values{
			"types":            {conversationTypes},
			"exclude_archived": {"true"},
			"limit":            {"200"},
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		var resp conversationsListResponse
		if err := c.get(ctx, "conversations.list", params, &resp); err != nil {
			return nil, fmt.Errorf("conversations.list: %w", err)
		}
		if !resp.OK {
			return nil, fmt.Errorf("conversations.list API error: %s", resp.Error)
		}

		for _, ch := range resp.Channels {
			if !c.include(ch.ID) {
				continue
			}
			// Struct tags are ignored for conversions (Go spec §Conversions, Go 1.8+).
			all = append(all, Conversation(ch))
		}

		if resp.ResponseMetadata.NextCursor == "" {
			break
		}
		cursor = resp.ResponseMetadata.NextCursor
	}

	return all, nil
}

// FetchMessages implements Client.
func (c *HTTPClient) FetchMessages(ctx context.Context, channelID string, from, to time.Time) ([]Message, error) {
	var all []Message
	cursor := ""

	for {
		params := url.Values{
			"channel": {channelID},
			"oldest":  {toSlackTS(from)},
			"latest":  {toSlackTS(to)},
			"limit":   {"200"},
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		var resp conversationsHistoryResponse
		if err := c.get(ctx, "conversations.history", params, &resp); err != nil {
			return nil, fmt.Errorf("conversations.history(%s): %w", channelID, err)
		}
		if !resp.OK {
			return nil, fmt.Errorf("conversations.history(%s) API error: %s", channelID, resp.Error)
		}

		for _, m := range resp.Messages {
			t, err := parseSlackTS(m.TS)
			if err != nil {
				continue // skip malformed timestamps
			}
			all = append(all, Message{
				TS:       m.TS,
				UserID:   m.User,
				Text:     m.Text,
				Time:     t,
				ThreadTS: m.ThreadTS,
			})
		}

		if !resp.HasMore {
			break
		}
		cursor = resp.ResponseMetadata.NextCursor
	}

	return all, nil
}

// include returns true if the channel should be included after whitelist/blacklist logic.
func (c *HTTPClient) include(id string) bool {
	if c.blacklist[id] {
		return false
	}
	if len(c.whitelist) > 0 {
		return c.whitelist[id]
	}
	return true
}

// get performs a GET request to the Slack API, decoding the JSON response into out.
func (c *HTTPClient) get(ctx context.Context, method string, params url.Values, out interface{}) error {
	endpoint := c.baseURL + "/" + method + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.xoxc)
	req.Header.Set("Cookie", "d="+c.xoxd)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close of read body

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// toSlackTS converts a time.Time to a Slack timestamp string.
func toSlackTS(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + ".000000"
}

// parseSlackTS parses a Slack timestamp (e.g. "1711929600.123456") into a time.Time.
func parseSlackTS(ts string) (time.Time, error) {
	// The integer part is Unix seconds.
	for i, c := range ts {
		if c == '.' {
			secs, err := strconv.ParseInt(ts[:i], 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse slack ts %q: %w", ts, err)
			}
			return time.Unix(secs, 0).UTC(), nil
		}
	}
	// No dot — treat the whole thing as seconds.
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse slack ts %q: %w", ts, err)
	}
	return time.Unix(secs, 0).UTC(), nil
}

// toSet converts a slice to a map for O(1) lookup.
func toSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

// --- API response types ---

type responseMetadata struct {
	NextCursor string `json:"next_cursor"`
}

type conversationsListResponse struct {
	OK               bool             `json:"ok"`
	Error            string           `json:"error"`
	Channels         []channelJSON    `json:"channels"`
	ResponseMetadata responseMetadata `json:"response_metadata"`
}

type channelJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsChannel  bool   `json:"is_channel"`
	IsIM       bool   `json:"is_im"`
	IsMPIM     bool   `json:"is_mpim"`
	IsPrivate  bool   `json:"is_private"`
	NumMembers int    `json:"num_members"`
}

type conversationsHistoryResponse struct {
	OK               bool             `json:"ok"`
	Error            string           `json:"error"`
	Messages         []messageJSON    `json:"messages"`
	HasMore          bool             `json:"has_more"`
	ResponseMetadata responseMetadata `json:"response_metadata"`
}

type messageJSON struct {
	TS       string `json:"ts"`
	User     string `json:"user"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts"`
}

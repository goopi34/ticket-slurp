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

// BrowserHeaders configures the HTTP headers used to mimic a real browser
// session. Empty fields fall back to defaults that resemble a recent Chrome
// on Linux. For best results, copy the values from the same browser session
// the xoxc/xoxd tokens were extracted from (devtools → Network → "Copy as
// cURL" on any slack.com/api request).
type BrowserHeaders struct {
	UserAgent       string
	Accept          string
	AcceptLanguage  string
	Origin          string
	Referer         string
	SecChUA         string
	SecChUAMobile   string
	SecChUAPlatform string
	// ExtraCookies is appended to the Cookie header after the required
	// "d=<xoxd>" cookie, separated by "; ". Useful for forwarding additional
	// session cookies (d-s, lc, b, x, etc.) so the request matches the
	// browser's full Cookie header.
	ExtraCookies string
}

// Default values for BrowserHeaders. Chosen to look like a current Chrome on
// Linux; users should override these to match their actual browser.
const (
	defaultUserAgent       = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	defaultAccept          = "*/*"
	defaultAcceptLanguage  = "en-US,en;q=0.9"
	defaultOrigin          = "https://app.slack.com"
	defaultReferer         = "https://app.slack.com/"
	defaultSecChUA         = `"Chromium";v="145", "Not_A Brand";v="24", "Google Chrome";v="145"`
	defaultSecChUAMobile   = "?0"
	defaultSecChUAPlatform = `"Linux"`
)

// withDefaults returns a copy of b with any empty fields replaced by the
// package defaults.
func (b BrowserHeaders) withDefaults() BrowserHeaders {
	if b.UserAgent == "" {
		b.UserAgent = defaultUserAgent
	}
	if b.Accept == "" {
		b.Accept = defaultAccept
	}
	if b.AcceptLanguage == "" {
		b.AcceptLanguage = defaultAcceptLanguage
	}
	if b.Origin == "" {
		b.Origin = defaultOrigin
	}
	if b.Referer == "" {
		b.Referer = defaultReferer
	}
	if b.SecChUA == "" {
		b.SecChUA = defaultSecChUA
	}
	if b.SecChUAMobile == "" {
		b.SecChUAMobile = defaultSecChUAMobile
	}
	if b.SecChUAPlatform == "" {
		b.SecChUAPlatform = defaultSecChUAPlatform
	}
	return b
}

// HTTPClient implements Client using Slack's HTTP API with xoxc/xoxd credentials.
type HTTPClient struct {
	baseURL   string
	xoxc      string
	xoxd      string
	whitelist map[string]bool
	blacklist map[string]bool
	browser   BrowserHeaders
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
		browser:   BrowserHeaders{}.withDefaults(),
		http:      httpClient,
	}
}

// WithBaseURL overrides the API base URL (used in tests).
func (c *HTTPClient) WithBaseURL(u string) *HTTPClient {
	c.baseURL = u
	return c
}

// WithBrowserHeaders overrides the browser-identifying headers. Empty fields
// in b retain their existing values, so callers can override only what they
// care about.
func (c *HTTPClient) WithBrowserHeaders(b BrowserHeaders) *HTTPClient {
	if b.UserAgent != "" {
		c.browser.UserAgent = b.UserAgent
	}
	if b.Accept != "" {
		c.browser.Accept = b.Accept
	}
	if b.AcceptLanguage != "" {
		c.browser.AcceptLanguage = b.AcceptLanguage
	}
	if b.Origin != "" {
		c.browser.Origin = b.Origin
	}
	if b.Referer != "" {
		c.browser.Referer = b.Referer
	}
	if b.SecChUA != "" {
		c.browser.SecChUA = b.SecChUA
	}
	if b.SecChUAMobile != "" {
		c.browser.SecChUAMobile = b.SecChUAMobile
	}
	if b.SecChUAPlatform != "" {
		c.browser.SecChUAPlatform = b.SecChUAPlatform
	}
	if b.ExtraCookies != "" {
		c.browser.ExtraCookies = b.ExtraCookies
	}
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
	cookie := "d=" + c.xoxd
	if c.browser.ExtraCookies != "" {
		cookie += "; " + c.browser.ExtraCookies
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", c.browser.Accept)
	req.Header.Set("Accept-Language", c.browser.AcceptLanguage)
	req.Header.Set("User-Agent", c.browser.UserAgent)
	req.Header.Set("Origin", c.browser.Origin)
	req.Header.Set("Referer", c.browser.Referer)
	req.Header.Set("sec-ch-ua", c.browser.SecChUA)
	req.Header.Set("sec-ch-ua-mobile", c.browser.SecChUAMobile)
	req.Header.Set("sec-ch-ua-platform", c.browser.SecChUAPlatform)
	// Browser-driven fetch metadata — invariant for XHR calls from the Slack
	// web app, so they're hardcoded rather than parameterized.
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-site")

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

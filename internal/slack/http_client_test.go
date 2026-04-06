package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ticket-slurp/internal/slack"
)

// handler is a simple function-based http.Handler.
type handler func(w http.ResponseWriter, r *http.Request)

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h(w, r) }

// newClient creates a test HTTPClient pointed at the given test server base URL.
func newClient(t *testing.T, baseURL string, whitelist, blacklist []string) *slack.HTTPClient {
	t.Helper()
	c := slack.NewHTTPClient("xoxc-test", "xoxd-test", whitelist, blacklist, nil)
	c.WithBaseURL(baseURL)
	return c
}

// --- conversations.list tests ---

func TestListConversations_Basic(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.list" {
			http.NotFound(w, r)
			return
		}
		writeJSON(t, w, map[string]interface{}{
			"ok": true,
			"channels": []map[string]interface{}{
				{"id": "C001", "name": "general", "is_channel": true},
				{"id": "C002", "name": "dev", "is_channel": true},
			},
			"response_metadata": map[string]string{"next_cursor": ""},
		})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	convs, err := client.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(convs))
	}
}

func TestListConversations_Pagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, r *http.Request) {
		calls++
		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			writeJSON(t, w, map[string]interface{}{
				"ok":                true,
				"channels":          []map[string]interface{}{{"id": "C001", "name": "a", "is_channel": true}},
				"response_metadata": map[string]string{"next_cursor": "page2"},
			})
		} else {
			writeJSON(t, w, map[string]interface{}{
				"ok":                true,
				"channels":          []map[string]interface{}{{"id": "C002", "name": "b", "is_channel": true}},
				"response_metadata": map[string]string{"next_cursor": ""},
			})
		}
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	convs, err := client.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 2 {
		t.Errorf("expected 2 conversations, got %d", len(convs))
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls (pagination), got %d", calls)
	}
}

func TestListConversations_APIError(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{"ok": false, "error": "not_authed"})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	_, err := client.ListConversations(context.Background())
	if err == nil {
		t.Fatal("expected error for API error response, got nil")
	}
}

func TestListConversations_Whitelist(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"ok": true,
			"channels": []map[string]interface{}{
				{"id": "C001", "name": "general", "is_channel": true},
				{"id": "C002", "name": "dev", "is_channel": true},
				{"id": "C003", "name": "random", "is_channel": true},
			},
			"response_metadata": map[string]string{"next_cursor": ""},
		})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, []string{"C001", "C003"}, nil)
	convs, err := client.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 2 {
		t.Errorf("expected 2 conversations after whitelist, got %d", len(convs))
	}
	for _, c := range convs {
		if c.ID == "C002" {
			t.Error("C002 should have been excluded by whitelist")
		}
	}
}

func TestListConversations_Blacklist(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"ok": true,
			"channels": []map[string]interface{}{
				{"id": "C001", "name": "general", "is_channel": true},
				{"id": "C002", "name": "dev", "is_channel": true},
			},
			"response_metadata": map[string]string{"next_cursor": ""},
		})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, []string{"C001"})
	convs, err := client.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convs) != 1 || convs[0].ID != "C002" {
		t.Errorf("expected only C002 after blacklist, got %v", convs)
	}
}

func TestListConversations_AuthHeaders(t *testing.T) {
	var gotAuth, gotCookie string
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		writeJSON(t, w, map[string]interface{}{
			"ok": true, "channels": []interface{}{},
			"response_metadata": map[string]string{"next_cursor": ""},
		})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	if _, err := client.ListConversations(context.Background()); err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if gotAuth != "Bearer xoxc-test" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "Bearer xoxc-test")
	}
	if gotCookie != "d=xoxd-test" {
		t.Errorf("Cookie header: got %q, want %q", gotCookie, "d=xoxd-test")
	}
}

// --- FetchMessages tests ---

func TestFetchMessages_Basic(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"ok": true,
			"messages": []map[string]interface{}{
				{"ts": "1711929600.000000", "user": "U001", "text": "hello"},
				{"ts": "1711929700.000000", "user": "U002", "text": "world"},
			},
			"has_more":          false,
			"response_metadata": map[string]string{"next_cursor": ""},
		})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	msgs, err := client.FetchMessages(context.Background(), "C001",
		time.Unix(1711929500, 0), time.Unix(1711929800, 0))
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestFetchMessages_Pagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, r *http.Request) {
		calls++
		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			writeJSON(t, w, map[string]interface{}{
				"ok":                true,
				"messages":          []map[string]interface{}{{"ts": "1711929600.000000", "user": "U001", "text": "a"}},
				"has_more":          true,
				"response_metadata": map[string]string{"next_cursor": "page2"},
			})
		} else {
			writeJSON(t, w, map[string]interface{}{
				"ok":                true,
				"messages":          []map[string]interface{}{{"ts": "1711929700.000000", "user": "U001", "text": "b"}},
				"has_more":          false,
				"response_metadata": map[string]string{"next_cursor": ""},
			})
		}
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	msgs, err := client.FetchMessages(context.Background(), "C001",
		time.Unix(1711929500, 0), time.Unix(1711929800, 0))
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	if calls != 2 {
		t.Errorf("expected 2 API calls, got %d", calls)
	}
}

func TestFetchMessages_APIError(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{"ok": false, "error": "channel_not_found"})
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	_, err := client.FetchMessages(context.Background(), "C999",
		time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestFetchMessages_HTTPError(t *testing.T) {
	srv := httptest.NewServer(handler(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newClient(t, srv.URL, nil, nil)
	_, err := client.FetchMessages(context.Background(), "C001",
		time.Now().Add(-time.Hour), time.Now())
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

// writeJSON serialises v as JSON into w.
func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("writeJSON: %v", err)
	}
}

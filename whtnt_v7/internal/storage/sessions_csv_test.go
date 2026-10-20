package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rnldo.dev/wnshell/internal/engine"
)

func TestSessionsCSVCompressedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.csv")
	metadata := map[string]any{
		"email":                 "roundtrip@example.com",
		"username":              "roundtrip",
		"whtnt_session_id":      strings.Repeat("session-", 50),
		"whtnt_app_session_id":  strings.Repeat("app-", 50),
		"whtnt_request_id":      strings.Repeat("request-", 50),
		"whtnt_session_token":   strings.Repeat("token-", 50),
		"checkout_orders":       []string{"order_a", "order_b"},
		"checkout_products":     []map[string]any{{"ProductName": "Test Product", "ProductPrice": 1000}},
		"repeated_for_compress": strings.Repeat("same-value-", 200),
	}
	records := []engine.SessionRecord{
		{
			ID:        "sess_123",
			AccountID: "acct_123",
			Cookies: []engine.CookieRecord{
				{Name: "__Secure-access-token", Value: strings.Repeat("access.", 100), Domain: "www.whatnot.com", Path: "/", Secure: true, HTTPOnly: true},
				{Name: "__Secure-refresh-token", Value: strings.Repeat("refresh.", 100), Domain: "www.whatnot.com", Path: "/", Secure: true, HTTPOnly: true},
			},
			Metadata:  metadata,
			CreatedAt: time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 27, 13, 0, 0, 0, time.UTC),
		},
	}

	if err := saveSessionsCSV(path, records); err != nil {
		t.Fatalf("saveSessionsCSV: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved csv: %v", err)
	}
	if !strings.Contains(string(raw), "gz:") {
		t.Fatalf("expected compressed gz fields in csv, got:\n%s", string(raw))
	}

	loaded, err := loadSessionsCSV(path)
	if err != nil {
		t.Fatalf("loadSessionsCSV: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d records, want 1", len(loaded))
	}
	if loaded[0].ID != records[0].ID || loaded[0].AccountID != records[0].AccountID {
		t.Fatalf("loaded identity mismatch: %#v", loaded[0])
	}
	if got := cookieValue(loaded[0].Cookies, "__Secure-access-token"); got != records[0].Cookies[0].Value {
		t.Fatalf("access token mismatch after decompress")
	}
	if got := metadataString(loaded[0].Metadata, "whtnt_session_token"); got != metadata["whtnt_session_token"] {
		t.Fatalf("session token mismatch after decompress")
	}
	if got := metadataString(loaded[0].Metadata, "email"); got != "roundtrip@example.com" {
		t.Fatalf("email mismatch after decompress: %q", got)
	}
}

func TestSessionsCSVLoadsPlainJSONRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.csv")
	plain := `id,account_id,email,username,proxy,ip,access_token,refresh_token,created_at,updated_at,cookies_json,metadata_json
sess_plain,acct_plain,plain@example.com,plain,,,access,refresh,2026-04-27T12:00:00Z,2026-04-27T13:00:00Z,"[{""name"":""__Secure-access-token"",""value"":""access""}]","{""email"":""plain@example.com"",""whtnt_session_token"":""plain-token""}"
`
	if err := os.WriteFile(path, []byte(plain), 0644); err != nil {
		t.Fatalf("write plain csv: %v", err)
	}

	loaded, err := loadSessionsCSV(path)
	if err != nil {
		t.Fatalf("loadSessionsCSV plain: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d records, want 1", len(loaded))
	}
	if got := cookieValue(loaded[0].Cookies, "__Secure-access-token"); got != "access" {
		t.Fatalf("plain access token mismatch: %q", got)
	}
	if got := metadataString(loaded[0].Metadata, "whtnt_session_token"); got != "plain-token" {
		t.Fatalf("plain metadata mismatch: %q", got)
	}
}

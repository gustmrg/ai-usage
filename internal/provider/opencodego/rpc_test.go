package opencodego

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gustmrg/ai-usage/internal/provider"
)

const usagePageFixture = `<!DOCTYPE html>
<html><body>
<script>
window.__data = {
  subscription: {
    rollingUsage:{status:"ok",resetInSec:9600,usagePercent:12},
    weeklyUsage:{status:"ok",resetInSec:432000,usagePercent:5.5},
    monthlyUsage:{status:"rate-limited",resetInSec:2000000,usagePercent:100},
    useBalance:false
  }
};
</script>
</body></html>`

func TestParseUsagePage(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	windows, err := parseUsagePage(usagePageFixture, now)
	if err != nil {
		t.Fatalf("parseUsagePage: %v", err)
	}
	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}
	rolling := windows[0]
	if rolling.Kind != "session" || rolling.Label != "5-hour" {
		t.Errorf("unexpected rolling window identity: %s/%s", rolling.Kind, rolling.Label)
	}
	if *rolling.UsedPercent != 12 {
		t.Errorf("rolling percent = %v, want 12", *rolling.UsedPercent)
	}
	if want := now.Add(9600 * time.Second); !rolling.ResetsAt.Equal(want) {
		t.Errorf("rolling reset = %v, want %v", rolling.ResetsAt, want)
	}
	if *rolling.DurationSeconds != rollingWindowSeconds {
		t.Errorf("rolling duration = %v, want %d", *rolling.DurationSeconds, rollingWindowSeconds)
	}
	weekly := windows[1]
	if *weekly.UsedPercent != 5.5 {
		t.Errorf("weekly percent = %v, want 5.5", *weekly.UsedPercent)
	}
	monthly := windows[2]
	if *monthly.UsedPercent != 100 {
		t.Errorf("monthly percent = %v, want 100", *monthly.UsedPercent)
	}
	if monthly.DurationSeconds != nil {
		t.Errorf("monthly duration should be nil, got %v", *monthly.DurationSeconds)
	}
}

func TestParseUsagePageQuotedKeys(t *testing.T) {
	page := `{"rollingUsage":{"status":"ok","resetInSec":100,"usagePercent":42}}`
	windows, err := parseUsagePage(page, time.Now())
	if err != nil {
		t.Fatalf("parseUsagePage: %v", err)
	}
	if len(windows) != 1 || *windows[0].UsedPercent != 42 {
		t.Fatalf("unexpected windows: %+v", windows)
	}
}

func TestParseUsagePageMissing(t *testing.T) {
	if _, err := parseUsagePage(`<html><body>nothing here</body></html>`, time.Now()); err == nil {
		t.Fatal("expected error for page without usage fields")
	}
}

func TestParseWorkspaceIDs(t *testing.T) {
	ids := parseWorkspaceIDs(`{id:"wrk_abc123",name:"team"},{id:"wrk_abc123"},{id:"wrk_def456"}`)
	if len(ids) != 2 || ids[0] != "wrk_abc123" || ids[1] != "wrk_def456" {
		t.Fatalf("unexpected ids: %v", ids)
	}
	ids = parseWorkspaceIDs(`{"workspaces":[{"id":"wrk_json1"}]}`)
	if len(ids) != 1 || ids[0] != "wrk_json1" {
		t.Fatalf("unexpected ids from JSON: %v", ids)
	}
	if ids := parseWorkspaceIDs(`no workspaces`); len(ids) != 0 {
		t.Fatalf("expected no ids, got %v", ids)
	}
}

func TestNormalizeWorkspaceID(t *testing.T) {
	cases := map[string]string{
		"wrk_abc123":     "wrk_abc123",
		"  wrk_abc123  ": "wrk_abc123",
		"https://opencode.ai/workspace/wrk_abc123":    "wrk_abc123",
		"https://opencode.ai/workspace/wrk_abc123/go": "wrk_abc123",
		"":     "",
		"team": "",
	}
	for input, want := range cases {
		if got := normalizeWorkspaceID(input); got != want {
			t.Errorf("normalizeWorkspaceID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFetchRPC(t *testing.T) {
	var sawWorkspaceRequest, sawUsageRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "auth=abc" {
			t.Errorf("missing cookie header on %s", r.URL.Path)
		}
		switch {
		case r.URL.Path == "/_server":
			sawWorkspaceRequest = true
			if r.URL.Query().Get("id") != workspacesServerID {
				t.Errorf("server function id = %q, want %q", r.URL.Query().Get("id"), workspacesServerID)
			}
			if r.Header.Get("X-Server-Id") != workspacesServerID {
				t.Errorf("missing X-Server-Id header")
			}
			fmt.Fprint(w, `{id:"wrk_test1",name:"personal"}`)
		case r.URL.Path == "/workspace/wrk_test1/go":
			sawUsageRequest = true
			fmt.Fprint(w, usagePageFixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("OPENCODE_AUTH_COOKIE", "abc")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	t.Setenv("OPENCODE_WORKSPACE_ID", "")

	client := New(server.Client())
	client.BaseURL = server.URL
	snapshot, err := client.Fetch(context.Background(), mustCacheKey(t, client))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !sawWorkspaceRequest || !sawUsageRequest {
		t.Fatalf("expected workspace and usage requests, got workspace=%v usage=%v", sawWorkspaceRequest, sawUsageRequest)
	}
	if snapshot.Provider != "opencodego" || snapshot.Plan != "OpenCode Go" {
		t.Errorf("unexpected snapshot identity: %s/%s", snapshot.Provider, snapshot.Plan)
	}
	if len(snapshot.Windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(snapshot.Windows))
	}
}

func TestFetchRPCWorkspaceOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_server" {
			t.Fatal("workspace lookup should be skipped when OPENCODE_WORKSPACE_ID is set")
		}
		if r.URL.Path != "/workspace/wrk_override/go" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, usagePageFixture)
	}))
	defer server.Close()

	t.Setenv("OPENCODE_AUTH_COOKIE", "abc")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	t.Setenv("OPENCODE_WORKSPACE_ID", "wrk_override")

	client := New(server.Client())
	client.BaseURL = server.URL
	if _, err := client.Fetch(context.Background(), mustCacheKey(t, client)); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestFetchRPCSignedOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>Please login to continue</body></html>`)
	}))
	defer server.Close()

	t.Setenv("OPENCODE_AUTH_COOKIE", "expired")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")

	client := New(server.Client())
	client.BaseURL = server.URL
	_, err := client.Fetch(context.Background(), mustCacheKey(t, client))
	if err == nil || !strings.Contains(err.Error(), "invalid or expired") {
		t.Fatalf("expected credentials error, got %v", err)
	}
}

func TestDetectWithoutCookie(t *testing.T) {
	t.Setenv("OPENCODE_AUTH_COOKIE", "")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	client := New(nil)
	detection := client.Detect()
	if detection.Available {
		t.Fatal("expected provider to be unavailable without a session cookie")
	}
	if detection.Detail != "set OPENCODE_AUTH_COOKIE" {
		t.Fatalf("unexpected setup detail: %q", detection.Detail)
	}
}

func TestCacheKeyWithoutCookie(t *testing.T) {
	t.Setenv("OPENCODE_AUTH_COOKIE", "")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	client := New(nil)
	if _, err := client.CacheKey(); err == nil || !strings.Contains(err.Error(), "set OPENCODE_AUTH_COOKIE") {
		t.Fatalf("expected missing-cookie error, got %v", err)
	}
}

func TestCacheKeyDoesNotReuseLegacyCookieSnapshots(t *testing.T) {
	t.Setenv("OPENCODE_AUTH_COOKIE", "abc")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	client := New(nil)
	key, err := client.CacheKey()
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := provider.CacheFingerprint("cookie:session=abc")
	if key == legacyKey {
		t.Fatal("console cache key must not reuse legacy local-fallback snapshots")
	}
}

func TestCookieHeader(t *testing.T) {
	tests := []struct {
		name       string
		auth       string
		session    string
		wantHeader string
		wantSource string
		wantError  string
	}{
		{name: "auth value", auth: "secret", wantHeader: "auth=secret", wantSource: "OPENCODE_AUTH_COOKIE"},
		{name: "auth pair", auth: "auth=secret", wantHeader: "auth=secret", wantSource: "OPENCODE_AUTH_COOKIE"},
		{name: "legacy raw value", session: "secret", wantHeader: "auth=secret", wantSource: "OPENCODE_SESSION_COOKIE"},
		{name: "legacy auth pair", session: "auth=secret", wantHeader: "auth=secret", wantSource: "OPENCODE_SESSION_COOKIE"},
		{name: "legacy full header", session: "other=1; auth=secret; theme=dark", wantHeader: "auth=secret", wantSource: "OPENCODE_SESSION_COOKIE"},
		{name: "legacy header label", session: "Cookie: auth=secret", wantHeader: "auth=secret", wantSource: "OPENCODE_SESSION_COOKIE"},
		{name: "missing auth", session: "other=1; theme=dark", wantError: "does not contain"},
		{name: "line break", auth: "secret\ninjected=1", wantError: "line break"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_AUTH_COOKIE", tc.auth)
			t.Setenv("OPENCODE_SESSION_COOKIE", tc.session)
			header, source, err := cookieHeader()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if header != tc.wantHeader || source != tc.wantSource {
				t.Fatalf("cookieHeader() = %q, %q; want %q, %q", header, source, tc.wantHeader, tc.wantSource)
			}
		})
	}
}

func TestFetchRPCAuthRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://auth.example.test/login", http.StatusFound)
	}))
	defer server.Close()

	t.Setenv("OPENCODE_AUTH_COOKIE", "expired")
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	t.Setenv("OPENCODE_WORKSPACE_ID", "wrk_test")

	client := New(server.Client())
	client.BaseURL = server.URL
	_, err := client.Fetch(context.Background(), mustCacheKey(t, client))
	if err == nil || provider.Kind(err) != provider.ErrorCredentials {
		t.Fatalf("expected credentials error, got %v", err)
	}
}

func mustCacheKey(t *testing.T, client *Client) string {
	t.Helper()
	key, err := client.CacheKey()
	if err != nil {
		t.Fatalf("CacheKey: %v", err)
	}
	return key
}

func TestLooksSignedOut(t *testing.T) {
	if !looksSignedOut(`actor of type "public" cannot access`) {
		t.Error("expected signed-out detection")
	}
	if looksSignedOut(usagePageFixture) {
		t.Error("usage page falsely detected as signed out")
	}
}

package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseUsageClassifiesWeeklyOnlyByDuration(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	snapshot, err := parseUsage([]byte(`{
      "plan_type":"prolite",
      "rate_limit":{"primary_window":{"used_percent":29.4,"limit_window_seconds":604800,"reset_at":1780000000}}
    }`), "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Windows) != 1 || snapshot.Windows[0].Kind != "weekly" {
		t.Fatalf("windows = %#v, want one weekly window", snapshot.Windows)
	}
	if got := *snapshot.Windows[0].UsedPercent; got != 29 {
		t.Fatalf("percent = %v, want 29", got)
	}
}

func TestParseUsageSupportsCreditsAndResetAfter(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	snapshot, err := parseUsage([]byte(`{
      "rate_limit":{"primary_window":{"used_percent":"61","limit_window_seconds":"18000","reset_after_seconds":3600}},
      "credits":{"balance":12.4,"has_credits":true,"unlimited":false,"approx_local_messages":[10,20]}
    }`), "plus", now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Plan != "ChatGPT Plus" || snapshot.Credits.Balance != "$12.40" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	wantReset := now.Add(time.Hour)
	if snapshot.Windows[0].ResetsAt == nil || !snapshot.Windows[0].ResetsAt.Equal(wantReset) {
		t.Fatalf("reset = %v, want %v", snapshot.Windows[0].ResetsAt, wantReset)
	}
}

func TestParseUsageKeepsUnknownAndAdditionalWindowsTruthful(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	snapshot, err := parseUsage([]byte(`{
      "rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":3600}},
      "additional_rate_limits":[{"limit_name":"Code review","rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":604800}}}]
    }`), "plus", now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Windows[0].Kind != "window_3600_0" || snapshot.Windows[0].Label != "1-hour" {
		t.Fatalf("unknown window = %#v", snapshot.Windows[0])
	}
	if snapshot.Windows[1].Kind != "additional_code_review" || snapshot.Windows[1].Label != "Code review" {
		t.Fatalf("additional window = %#v", snapshot.Windows[1])
	}
}

func TestAuthSavePreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	token := fakeJWT(map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	body := map[string]any{
		"tokens":            map[string]any{"access_token": token, "refresh_token": "refresh", "id_token": token, "future_token_field": "keep"},
		"future_root_field": map[string]any{"keep": true},
	}
	data, _ := json.Marshal(body)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := ReadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	auth.Update("new-access", "new-refresh", "", time.Now().Add(time.Hour))
	if err := auth.Save(path); err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["future_root_field"] == nil || saved["tokens"].(map[string]any)["future_token_field"] != "keep" {
		t.Fatalf("unknown fields were not preserved: %#v", saved)
	}
}

func TestAuthSaveRefusesToOverwriteConcurrentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	token := fakeJWT(map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	data, _ := json.Marshal(map[string]any{"tokens": map[string]any{"access_token": token, "refresh_token": "refresh", "id_token": token}})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := ReadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	auth.Update("new-access", "new-refresh", "", time.Now().Add(time.Hour))
	if err := auth.Save(path); err == nil {
		t.Fatal("Save() overwrote concurrently changed credentials")
	}
}

func TestDefaultAuthPathUsesCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/custom-codex")
	path, err := DefaultAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/custom-codex/auth.json" {
		t.Fatalf("path = %q", path)
	}
}

func TestRefreshRetainsExistingOAuthScopes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	token := fakeJWT(map[string]any{"exp": time.Now().Add(-time.Hour).Unix(), "sub": "user"})
	data, _ := json.Marshal(map[string]any{"tokens": map[string]any{"access_token": token, "refresh_token": "refresh", "id_token": token}})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := ReadAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if _, exists := request["scope"]; exists {
			t.Errorf("refresh request narrowed OAuth scopes: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","expires_in":3600}`))
	}))
	defer server.Close()
	client := New(server.Client(), path)
	client.TokenURL = server.URL
	if err := client.refresh(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
}

func fakeJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(claims)
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

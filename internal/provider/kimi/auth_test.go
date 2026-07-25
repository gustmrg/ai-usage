package kimi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveOAuthLocationUsesKimiCodeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("KIMI_CODE_BASE_URL", "")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	location, err := resolveOAuthLocation()
	if err != nil {
		t.Fatal(err)
	}
	if location.tokenPath != filepath.Join(home, "credentials", "kimi-code.json") {
		t.Fatalf("tokenPath = %q", location.tokenPath)
	}
	if location.baseURL != defaultBaseURL || location.oauthHost != defaultOAuthHost {
		t.Fatalf("location = %#v", location)
	}
}

func TestResolveOAuthLocationScopesEndpointOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("KIMI_CODE_BASE_URL", "https://api.example.test/coding/v1/")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "https://auth.example.test/")
	location, err := resolveOAuthLocation()
	if err != nil {
		t.Fatal(err)
	}
	if location.storageName == "kimi-code" || filepath.Base(location.tokenPath) != location.storageName+".json" {
		t.Fatalf("override was not scoped: %#v", location)
	}
}

func TestRefreshOAuthUsesOfficialFormAndPersistsToken(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		received = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "access_token":"new-access",
          "refresh_token":"new-refresh",
          "expires_in":3600,
          "scope":"kimi-code",
          "token_type":"Bearer"
        }`))
	}))
	defer server.Close()

	location := oauthLocation{
		home:        home,
		storageName: "kimi-code",
		tokenPath:   filepath.Join(home, "credentials", "kimi-code.json"),
		oauthHost:   server.URL,
		baseURL:     defaultBaseURL,
	}
	if err := writeOAuthToken(location.tokenPath, oauthToken{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: now.Add(-time.Hour).Unix(), ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	client := New(server.Client())
	client.now = func() time.Time { return now }
	token, err := client.refreshOAuth(context.Background(), location)
	if err != nil {
		t.Fatal(err)
	}
	if received.Get("client_id") != oauthClientID || received.Get("grant_type") != "refresh_token" || received.Get("refresh_token") != "old-refresh" {
		t.Fatalf("refresh form = %#v", received)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" || token.ExpiresAt != now.Unix()+3600 {
		t.Fatalf("token = %#v", token)
	}
	saved, err := readOAuthToken(location.tokenPath)
	if err != nil || saved.RefreshToken != "new-refresh" {
		t.Fatalf("saved token = %#v, err = %v", saved, err)
	}
	if _, err := os.Stat(filepath.Join(home, "oauth", "kimi-code.lock")); !os.IsNotExist(err) {
		t.Fatalf("OAuth lock was not released: %v", err)
	}
}

func TestDetectPrefersSubscriptionOAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("KIMI_CODE_BASE_URL", "")
	t.Setenv("KIMI_CODE_OAUTH_HOST", "")
	t.Setenv("KIMI_OAUTH_HOST", "")
	t.Setenv("KIMI_API_KEY", "fallback-key")
	path := filepath.Join(home, "credentials", "kimi-code.json")
	if err := writeOAuthToken(path, oauthToken{AccessToken: testJWT("subscriber"), RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour).Unix(), ExpiresIn: 3600}); err != nil {
		t.Fatal(err)
	}
	client := New(nil)
	detection := client.Detect()
	if !detection.Available || detection.Detail == "KIMI_API_KEY fallback" {
		t.Fatalf("detection = %#v", detection)
	}
	key, err := client.CacheKey()
	if err != nil || key != oauthCacheKey(oauthToken{AccessToken: testJWT("subscriber")}) {
		t.Fatalf("cache key = %q, err = %v", key, err)
	}
}

func testJWT(subject string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{"sub": subject})
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

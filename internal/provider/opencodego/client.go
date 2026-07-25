// Package opencodego tracks OpenCode Go subscription usage.
//
// OpenCode exposes no public usage API, so the primary source is the
// dashboard itself: requests authenticated with the web session cookie
// (OPENCODE_AUTH_COOKIE) against the Solid Start server functions on
// opencode.ai, the same approach CodexBar uses.
package opencodego

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

const (
	rollingWindowSeconds = int64(5 * 60 * 60)
	weeklyWindowSeconds  = int64(7 * 24 * 60 * 60)
)

type Client struct {
	HTTP    *http.Client
	BaseURL string // defaults to https://opencode.ai; overridden in tests
	now     func() time.Time
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{HTTP: httpClient, now: time.Now}
}

func (c *Client) ID() string          { return "opencodego" }
func (c *Client) DisplayName() string { return "OpenCode Go" }
func (c *Client) AllowStaleCache() bool {
	return false
}

type credential struct {
	cookie      string
	workspaceID string
	cacheKey    string
}

func cookieInput() (string, string) {
	if value := strings.TrimSpace(os.Getenv("OPENCODE_AUTH_COOKIE")); value != "" {
		return value, "OPENCODE_AUTH_COOKIE"
	}
	if value := strings.TrimSpace(os.Getenv("OPENCODE_SESSION_COOKIE")); value != "" {
		return value, "OPENCODE_SESSION_COOKIE"
	}
	return "", ""
}

func cookieHeader() (string, string, error) {
	raw, source := cookieInput()
	if raw == "" {
		return "", "", errors.New("OpenCode Go credentials not found; set OPENCODE_AUTH_COOKIE")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return "", source, fmt.Errorf("%s contains a line break", source)
	}

	raw = strings.TrimSpace(strings.TrimPrefix(raw, "Cookie:"))
	if raw == "" {
		return "", source, fmt.Errorf("%s is empty", source)
	}

	if source == "OPENCODE_AUTH_COOKIE" {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "auth="))
		if raw == "" || strings.Contains(raw, ";") {
			return "", source, errors.New("OPENCODE_AUTH_COOKIE must contain only the value of the opencode.ai auth cookie")
		}
		return "auth=" + raw, source, nil
	}

	// OPENCODE_SESSION_COOKIE previously accepted a full Cookie request
	// header. Preserve that form, but also accept the raw auth-cookie value
	// so users copying from browser storage get the expected request.
	if strings.HasPrefix(raw, "auth=") {
		return raw, source, nil
	}
	if strings.Contains(raw, ";") {
		for part := range strings.SplitSeq(raw, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "auth=") {
				return part, source, nil
			}
		}
		return "", source, errors.New("OPENCODE_SESSION_COOKIE does not contain the opencode.ai auth cookie")
	}
	return "auth=" + raw, source, nil
}

func workspaceIDOverride() string {
	return normalizeWorkspaceID(os.Getenv("OPENCODE_WORKSPACE_ID"))
}

func (c *Client) resolveCredential() (credential, error) {
	cookie, _, err := cookieHeader()
	if err != nil {
		return credential{}, err
	}
	return credential{
		cookie:      cookie,
		workspaceID: workspaceIDOverride(),
		cacheKey:    provider.CacheFingerprint("console-v1:" + cookie),
	}, nil
}

func (c *Client) Detect() provider.Detection {
	_, source := cookieInput()
	if source == "" {
		return provider.Detection{Detail: "set OPENCODE_AUTH_COOKIE"}
	}
	return provider.Detection{Available: true, Detail: source}
}

func (c *Client) CacheKey() (string, error) {
	cred, err := c.resolveCredential()
	if err != nil {
		return "", err
	}
	return cred.cacheKey, nil
}

func (c *Client) Fetch(ctx context.Context, expectedCacheKey string) (model.Snapshot, error) {
	cred, err := c.resolveCredential()
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: err.Error(), Err: err}
	}
	if cred.cacheKey != expectedCacheKey {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "OpenCode Go credentials changed while loading usage; retry"}
	}
	return c.fetchRPC(ctx, cred)
}

func newSnapshot(now time.Time, windows []model.UsageWindow) model.Snapshot {
	return model.Snapshot{
		SchemaVersion: model.SchemaVersion,
		Provider:      "opencodego",
		Plan:          "OpenCode Go",
		CollectedAt:   now.UTC(),
		Windows:       windows,
	}
}

func percentOnlyWindow(kind, label string, pct float64, duration int64, reset time.Time) model.UsageWindow {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	reset = reset.UTC()
	window := model.UsageWindow{
		Kind:        kind,
		Label:       label,
		UsedPercent: &pct,
		ResetsAt:    &reset,
	}
	if duration > 0 {
		window.DurationSeconds = &duration
	}
	return window
}

var errMissingUsage = errors.New("missing usage fields")

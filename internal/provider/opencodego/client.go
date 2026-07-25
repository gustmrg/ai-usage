// Package opencodego tracks OpenCode Go subscription usage.
//
// OpenCode exposes no public usage API, so the primary source is the
// dashboard itself: requests authenticated with the web session cookie
// (OPENCODE_SESSION_COOKIE) against the Solid Start server functions on
// opencode.ai, the same approach CodexBar uses. When no cookie is
// configured, or the RPC path fails, it falls back to summing per-message
// costs from opencode's local SQLite database.
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

// Usage limits in whole cents, as published in the OpenCode Go docs.
const (
	rollingLimitCents = uint64(1200) // $12 per rolling 5 hours
	weeklyLimitCents  = uint64(3000) // $30 per week
	monthlyLimitCents = uint64(6000) // $60 per month

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

type credential struct {
	cookie      string
	workspaceID string
	dbPath      string
	cacheKey    string
	source      string
}

func sessionCookie() string {
	return strings.TrimSpace(os.Getenv("OPENCODE_SESSION_COOKIE"))
}

func workspaceIDOverride() string {
	return normalizeWorkspaceID(os.Getenv("OPENCODE_WORKSPACE_ID"))
}

func (c *Client) resolveCredential() (credential, error) {
	cred := credential{workspaceID: workspaceIDOverride()}
	if cookie := sessionCookie(); cookie != "" {
		cred.cookie = cookie
		cred.cacheKey = provider.CacheFingerprint("cookie:" + cookie)
		cred.source = "OPENCODE_SESSION_COOKIE"
		return cred, nil
	}
	path, err := defaultDBPath()
	if err != nil {
		return credential{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return credential{}, fmt.Errorf("OpenCode Go credentials not found; set OPENCODE_SESSION_COOKIE or use opencode on this machine")
	}
	cred.dbPath = path
	cred.cacheKey = provider.CacheFingerprint("localdb:" + path)
	cred.source = "local opencode database"
	return cred, nil
}

func (c *Client) Detect() provider.Detection {
	if sessionCookie() != "" {
		return provider.Detection{Available: true, Detail: "OPENCODE_SESSION_COOKIE"}
	}
	path, err := defaultDBPath()
	if err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return provider.Detection{Available: true, Detail: fmt.Sprintf("local opencode database (%s)", path)}
		}
	}
	return provider.Detection{Detail: "set OPENCODE_SESSION_COOKIE or use opencode on this machine"}
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
	if cred.cookie != "" {
		snapshot, rpcErr := c.fetchRPC(ctx, cred)
		if rpcErr == nil {
			return snapshot, nil
		}
		// Fall back to the local database when the RPC path fails (expired
		// cookie, deploy rotated the server IDs, offline, ...).
		if path, pathErr := defaultDBPath(); pathErr == nil {
			if _, statErr := os.Stat(path); statErr == nil {
				snapshot, dbErr := c.fetchLocal(path)
				if dbErr == nil {
					return snapshot, nil
				}
				return model.Snapshot{}, fmt.Errorf("rpc: %v; local fallback: %v", rpcErr, dbErr)
			}
		}
		return model.Snapshot{}, rpcErr
	}
	return c.fetchLocal(cred.dbPath)
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

func percentWindow(kind, label string, used, limit uint64, duration int64, reset time.Time) model.UsageWindow {
	pct := model.Percent(used, limit)
	reset = reset.UTC()
	window := model.UsageWindow{
		Kind:        kind,
		Label:       label,
		Used:        &used,
		Limit:       &limit,
		UsedPercent: &pct,
		ResetsAt:    &reset,
	}
	if duration > 0 {
		window.DurationSeconds = &duration
	}
	return window
}

// percentOnlyWindow is used by the RPC path, which reports percentages but
// no absolute amounts.
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

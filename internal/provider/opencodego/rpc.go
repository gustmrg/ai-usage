package opencodego

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

const (
	defaultBaseURL = "https://opencode.ai"
	// workspacesServerID is the build hash of the Solid Start server
	// function that lists workspaces (same one CodexBar uses). It can
	// rotate on console deploys; the local database fallback covers that.
	workspacesServerID = "def39973159c7f0483d8793a822b8dbb10d067e12c65455fcb4608459ba0234f"
	userAgent          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
)

var (
	workspaceIDPattern = regexp.MustCompile(`id\s*:\s*"(wrk_[^"]+)"`)
	workspaceIDLoose   = regexp.MustCompile(`wrk_[A-Za-z0-9]+`)
)

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultBaseURL
}

// httpClient returns a client that refuses redirects off the console host,
// so session cookies never leak to a third-party domain.
func (c *Client) httpClient() *http.Client {
	client := *c.HTTP
	base := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return http.ErrUseLastResponse
		}
		if base != nil {
			return base(req, via)
		}
		return nil
	}
	return &client
}

func (c *Client) fetchRPC(ctx context.Context, cred credential) (model.Snapshot, error) {
	workspaceID := cred.workspaceID
	if workspaceID == "" {
		var err error
		workspaceID, err = c.fetchWorkspaceID(ctx, cred.cookie)
		if err != nil {
			return model.Snapshot{}, err
		}
	}
	page, err := c.fetchUsagePage(ctx, cred.cookie, workspaceID)
	if err != nil {
		return model.Snapshot{}, err
	}
	windows, err := parseUsagePage(page, c.now())
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "OpenCode console schema drift", Err: err}
	}
	return newSnapshot(c.now(), windows), nil
}

func (c *Client) fetchWorkspaceID(ctx context.Context, cookie string) (string, error) {
	text, err := c.fetchServerFunction(ctx, cookie, http.MethodGet, "")
	if err != nil {
		return "", err
	}
	if looksSignedOut(text) {
		return "", &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "OpenCode session cookie is invalid or expired"}
	}
	ids := parseWorkspaceIDs(text)
	if len(ids) == 0 {
		// Some console builds only answer the POST form of server functions.
		fallback, postErr := c.fetchServerFunction(ctx, cookie, http.MethodPost, "[]")
		if postErr != nil {
			return "", postErr
		}
		if looksSignedOut(fallback) {
			return "", &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "OpenCode session cookie is invalid or expired"}
		}
		ids = parseWorkspaceIDs(fallback)
	}
	if len(ids) == 0 {
		return "", &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "OpenCode workspace id missing from console response"}
	}
	return ids[0], nil
}

// fetchServerFunction calls a Solid Start server function. GET encodes the
// function id (and optional JSON args) in the query string; POST sends the
// args as a JSON body.
func (c *Client) fetchServerFunction(ctx context.Context, cookie, method, args string) (string, error) {
	endpoint := c.baseURL() + "/_server"
	var body io.Reader
	if method == http.MethodGet {
		query := url.Values{"id": {workspacesServerID}}
		if args != "" {
			query.Set("args", args)
		}
		endpoint += "?" + query.Encode()
	} else if args != "" {
		body = strings.NewReader(args)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("X-Server-Id", workspacesServerID)
	req.Header.Set("X-Server-Instance", "server-fn:"+newUUID())
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", c.baseURL())
	req.Header.Set("Referer", c.baseURL()+"/")
	req.Header.Set("Accept", "text/javascript, application/json;q=0.9, */*;q=0.8")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doText(req)
}

func (c *Client) fetchUsagePage(ctx context.Context, cookie, workspaceID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/workspace/"+workspaceID+"/go", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	text, err := c.doText(req)
	if err != nil {
		return "", err
	}
	if looksSignedOut(text) {
		return "", &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "OpenCode session cookie is invalid or expired"}
	}
	return text, nil
}

func (c *Client) doText(req *http.Request) (string, error) {
	response, err := c.httpClient().Do(req)
	if err != nil {
		return "", &provider.Error{Kind: provider.ErrorTransport, Provider: c.ID(), Message: "OpenCode console request failed", Err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return "", err
	}
	if len(body) > 4<<20 {
		return "", &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "OpenCode console response exceeds 4 MiB"}
	}
	text := string(body)
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || looksSignedOut(text) {
			return "", &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), StatusCode: response.StatusCode, Message: "OpenCode session cookie is invalid or expired"}
		}
		return "", &provider.Error{Kind: provider.ErrorHTTP, Provider: c.ID(), StatusCode: response.StatusCode, Message: fmt.Sprintf("OpenCode console returned HTTP %d", response.StatusCode)}
	}
	return text, nil
}

func parseWorkspaceIDs(text string) []string {
	ids := []string{}
	for _, match := range workspaceIDPattern.FindAllStringSubmatch(text, -1) {
		ids = appendUnique(ids, match[1])
	}
	if len(ids) > 0 {
		return ids
	}
	// Fallback: walk a plain JSON response for wrk_ strings.
	var value any
	if json.Unmarshal([]byte(text), &value) != nil {
		return ids
	}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case string:
			if strings.HasPrefix(typed, "wrk_") {
				ids = appendUnique(ids, typed)
			}
		}
	}
	walk(value)
	return ids
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func normalizeWorkspaceID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "wrk_") && len(trimmed) > 4 {
		return trimmed
	}
	return workspaceIDLoose.FindString(trimmed)
}

func looksSignedOut(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "login") ||
		strings.Contains(lower, "sign in") ||
		strings.Contains(lower, "auth/authorize") ||
		strings.Contains(lower, "not associated with an account") ||
		strings.Contains(lower, `actor of type "public"`)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// The usage page embeds the server-rendered subscription data as a
// serialized JavaScript object, e.g.
// rollingUsage:{status:"ok",resetInSec:9600,usagePercent:12}.
// The keys may also appear quoted, so the patterns tolerate both forms.
var (
	rollingPercentPattern = regexp.MustCompile(`rollingUsage[^}]*?usagePercent"?\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	rollingResetPattern   = regexp.MustCompile(`rollingUsage[^}]*?resetInSec"?\s*:\s*([0-9]+)`)
	weeklyPercentPattern  = regexp.MustCompile(`weeklyUsage[^}]*?usagePercent"?\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	weeklyResetPattern    = regexp.MustCompile(`weeklyUsage[^}]*?resetInSec"?\s*:\s*([0-9]+)`)
	monthlyPercentPattern = regexp.MustCompile(`monthlyUsage[^}]*?usagePercent"?\s*:\s*([0-9]+(?:\.[0-9]+)?)`)
	monthlyResetPattern   = regexp.MustCompile(`monthlyUsage[^}]*?resetInSec"?\s*:\s*([0-9]+)`)
)

func parseUsagePage(text string, now time.Time) ([]model.UsageWindow, error) {
	rollingPct, ok := extractFloat(rollingPercentPattern, text)
	if !ok {
		return nil, errMissingUsage
	}
	rollingReset, ok := extractInt(rollingResetPattern, text)
	if !ok {
		return nil, errMissingUsage
	}
	windows := []model.UsageWindow{
		percentOnlyWindow("session", "5-hour", rollingPct, rollingWindowSeconds, now.Add(time.Duration(rollingReset)*time.Second)),
	}
	if pct, ok := extractFloat(weeklyPercentPattern, text); ok {
		if reset, ok := extractInt(weeklyResetPattern, text); ok {
			windows = append(windows, percentOnlyWindow("weekly", "Weekly", pct, weeklyWindowSeconds, now.Add(time.Duration(reset)*time.Second)))
		}
	}
	if pct, ok := extractFloat(monthlyPercentPattern, text); ok {
		if reset, ok := extractInt(monthlyResetPattern, text); ok {
			windows = append(windows, percentOnlyWindow("monthly", "Monthly", pct, 0, now.Add(time.Duration(reset)*time.Second)))
		}
	}
	return windows, nil
}

func extractFloat(pattern *regexp.Regexp, text string) (float64, bool) {
	match := pattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	return value, err == nil
}

func extractInt(pattern *regexp.Regexp, text string) (int64, bool) {
	match := pattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseInt(match[1], 10, 64)
	return value, err == nil
}

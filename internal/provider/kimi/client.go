package kimi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

type Client struct {
	HTTP     *http.Client
	UsageURL string
	now      func() time.Time
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{HTTP: httpClient, now: time.Now}
}

func (c *Client) ID() string          { return "kimi" }
func (c *Client) DisplayName() string { return "Kimi" }

func (c *Client) Detect() provider.Detection {
	location, err := resolveOAuthLocation()
	if err == nil {
		if _, tokenErr := readOAuthToken(location.tokenPath); tokenErr == nil {
			return provider.Detection{Available: true, Detail: fmt.Sprintf("Kimi Code OAuth (%s)", location.tokenPath)}
		} else if !errors.Is(tokenErr, os.ErrNotExist) {
			return provider.Detection{Detail: tokenErr.Error()}
		}
	}
	if strings.TrimSpace(os.Getenv("KIMI_API_KEY")) != "" {
		return provider.Detection{Available: true, Detail: "KIMI_API_KEY fallback"}
	}
	return provider.Detection{Detail: "Kimi subscription login not found; run `kimi login`"}
}

func (c *Client) CacheKey() (string, error) {
	location, err := resolveOAuthLocation()
	if err != nil {
		return "", err
	}
	if token, tokenErr := readOAuthToken(location.tokenPath); tokenErr == nil {
		return oauthCacheKey(token), nil
	} else if !errors.Is(tokenErr, os.ErrNotExist) {
		return "", tokenErr
	}
	key := strings.TrimSpace(os.Getenv("KIMI_API_KEY"))
	if key == "" {
		return "", fmt.Errorf("Kimi subscription login not found; run `kimi login`")
	}
	return provider.CacheFingerprint("api-key:" + key), nil
}

func (c *Client) Fetch(ctx context.Context, expectedCacheKey string) (model.Snapshot, error) {
	credential, err := c.resolveCredential(ctx)
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: err.Error(), Err: err}
	}
	if credential.cacheKey != expectedCacheKey {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "Kimi credentials changed while loading usage; retry"}
	}
	usageURL := credential.usageURL
	if c.UsageURL != "" {
		usageURL = c.UsageURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return model.Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.accessToken)
	req.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorTransport, Provider: c.ID(), Message: "Kimi usage request failed", Err: err}
	}
	defer response.Body.Close()
	body, err := readBody(response.Body)
	if err != nil {
		return model.Snapshot{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := fmt.Sprintf("Kimi API returned HTTP %d", response.StatusCode)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			message = "Kimi authentication failed"
		}
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorHTTP, Provider: c.ID(), StatusCode: response.StatusCode, Message: message}
	}
	snapshot, err := parseUsage(body, c.now())
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "Kimi API schema drift", Err: err}
	}
	return snapshot, nil
}

type response struct {
	User *struct {
		Membership *struct {
			Level string `json:"level"`
		} `json:"membership"`
	} `json:"user"`
	Usage  *usageDetail `json:"usage"`
	Limits []limit      `json:"limits"`
}

type limit struct {
	Window *struct {
		Duration    uint64 `json:"duration"`
		TimeUnit    string `json:"timeUnit"`
		TimeUnitAlt string `json:"time_unit"`
	} `json:"window"`
	Detail *usageDetail `json:"detail"`
}

type usageDetail struct {
	Limit      json.RawMessage `json:"limit"`
	Used       json.RawMessage `json:"used"`
	Remaining  json.RawMessage `json:"remaining"`
	ResetTime  string          `json:"resetTime"`
	ResetAt    string          `json:"resetAt"`
	ResetSnake string          `json:"reset_at"`
	ResetAlt   string          `json:"reset_time"`
}

func parseUsage(data []byte, now time.Time) (model.Snapshot, error) {
	var response response
	if err := json.Unmarshal(data, &response); err != nil {
		return model.Snapshot{}, err
	}
	if response.Usage == nil {
		return model.Snapshot{}, fmt.Errorf("missing usage block")
	}
	weekly, err := parseDetail(response.Usage, "weekly", "Weekly", 7*24*60*60)
	if err != nil {
		return model.Snapshot{}, err
	}
	plan := ""
	if response.User != nil && response.User.Membership != nil {
		plan = strings.TrimPrefix(response.User.Membership.Level, "LEVEL_")
		plan = strings.ToLower(plan)
		if plan != "" {
			plan = strings.ToUpper(plan[:1]) + plan[1:]
		}
	}
	snapshot := model.Snapshot{SchemaVersion: model.SchemaVersion, Provider: "kimi", Plan: plan, CollectedAt: now.UTC(), Windows: []model.UsageWindow{weekly}}
	if len(response.Limits) > 0 {
		for _, candidate := range response.Limits {
			if candidate.Window == nil || candidate.Detail == nil || !isFiveHour(candidate) {
				continue
			}
			window, err := parseDetail(candidate.Detail, "session", "5-hour", 5*60*60)
			if err != nil {
				return model.Snapshot{}, err
			}
			snapshot.Windows = append([]model.UsageWindow{window}, snapshot.Windows...)
			break
		}
	}
	return snapshot, nil
}

func isFiveHour(candidate limit) bool {
	unit := candidate.Window.TimeUnit
	if unit == "" {
		unit = candidate.Window.TimeUnitAlt
	}
	unit = strings.ToUpper(unit)
	return (candidate.Window.Duration == 300 && (unit == "TIME_UNIT_MINUTE" || unit == "MINUTE" || unit == "MINUTES")) ||
		(candidate.Window.Duration == 5 && (unit == "TIME_UNIT_HOUR" || unit == "HOUR" || unit == "HOURS"))
}

func parseDetail(detail *usageDetail, kind, label string, duration int64) (model.UsageWindow, error) {
	limit, err := exactUint(detail.Limit)
	if err != nil {
		return model.UsageWindow{}, fmt.Errorf("invalid limit: %w", err)
	}
	used, hasUsed, err := optionalUint(detail.Used)
	if err != nil {
		return model.UsageWindow{}, fmt.Errorf("invalid used value: %w", err)
	}
	remaining, hasRemaining, err := optionalUint(detail.Remaining)
	if err != nil {
		return model.UsageWindow{}, fmt.Errorf("invalid remaining value: %w", err)
	}
	if !hasUsed && !hasRemaining {
		return model.UsageWindow{}, fmt.Errorf("missing used and remaining values")
	}
	if !hasUsed {
		used = subtract(limit, remaining)
	}
	if !hasRemaining {
		remaining = subtract(limit, used)
	}
	pct := model.Percent(used, limit)
	resetText := firstNonEmpty(detail.ResetTime, detail.ResetAt, detail.ResetSnake, detail.ResetAlt)
	var reset *time.Time
	if resetText != "" {
		parsed, err := time.Parse(time.RFC3339, resetText)
		if err != nil {
			return model.UsageWindow{}, fmt.Errorf("invalid reset time: %w", err)
		}
		parsed = parsed.UTC()
		reset = &parsed
	}
	return model.UsageWindow{Kind: kind, Label: label, Used: &used, Limit: &limit, Remaining: &remaining, UsedPercent: &pct, DurationSeconds: &duration, ResetsAt: reset}, nil
}

func exactUint(raw json.RawMessage) (uint64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("missing integer")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return 0, err
	}
	return strconv.ParseUint(number.String(), 10, 64)
}

func optionalUint(raw json.RawMessage) (uint64, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, nil
	}
	value, err := exactUint(raw)
	return value, true, err
}

func subtract(left, right uint64) uint64 {
	if right > left {
		return 0
	}
	return left - right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func readBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 2<<20 {
		return nil, &provider.Error{Kind: provider.ErrorSchema, Provider: "kimi", Message: "Kimi response exceeds 2 MiB"}
	}
	return body, nil
}

package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

const (
	usageURL    = "https://chatgpt.com/backend-api/wham/usage"
	tokenURL    = "https://auth.openai.com/oauth/token"
	clientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	maxBodySize = 2 << 20
)

type Client struct {
	HTTP     *http.Client
	AuthPath string
	UsageURL string
	TokenURL string
	now      func() time.Time
}

func New(httpClient *http.Client, authPath string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{HTTP: httpClient, AuthPath: authPath, UsageURL: usageURL, TokenURL: tokenURL, now: time.Now}
}

func (c *Client) ID() string          { return "codex" }
func (c *Client) DisplayName() string { return "Codex" }

func (c *Client) Detect() provider.Detection {
	info, err := os.Stat(c.AuthPath)
	if err != nil || info.IsDir() {
		return provider.Detection{Detail: fmt.Sprintf("Codex credentials not found at %s; run `codex login`", c.AuthPath)}
	}
	return provider.Detection{Available: true, Detail: c.AuthPath}
}

func (c *Client) CacheKey() (string, error) {
	auth, err := ReadAuth(c.AuthPath)
	if err != nil {
		return "", err
	}
	return authCacheKey(auth), nil
}

func authCacheKey(auth *Auth) string {
	identity := auth.AccountID
	if identity == "" {
		if subject, ok := jwtClaims(auth.IDToken)["sub"].(string); ok {
			identity = subject
		} else {
			identity = auth.IDToken
		}
	}
	return provider.CacheFingerprint(identity)
}

func (c *Client) Fetch(ctx context.Context, expectedCacheKey string) (model.Snapshot, error) {
	auth, err := ReadAuth(c.AuthPath)
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: err.Error(), Err: err}
	}
	if authCacheKey(auth) != expectedCacheKey {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "Codex account changed while loading usage; retry"}
	}
	if auth.NeedsRefresh(c.now()) {
		if err := c.refresh(ctx, auth); err != nil {
			return model.Snapshot{}, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UsageURL, nil)
	if err != nil {
		return model.Snapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	req.Header.Set("User-Agent", "codex-cli")
	if auth.AccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", auth.AccountID)
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorTransport, Provider: c.ID(), Message: "Codex usage request failed", Err: err}
	}
	defer response.Body.Close()
	body, err := readBody(response.Body)
	if err != nil {
		return model.Snapshot{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := fmt.Sprintf("Codex API returned HTTP %d", response.StatusCode)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			message = "Codex authentication failed; run `codex login`"
		}
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorHTTP, Provider: c.ID(), StatusCode: response.StatusCode, Message: message}
	}
	snapshot, err := parseUsage(body, planFromJWT(auth.IDToken), c.now())
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "Codex API schema drift", Err: err}
	}
	currentCacheKey, err := c.CacheKey()
	if err != nil || currentCacheKey != expectedCacheKey {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "Codex account changed while loading usage; retry", Err: err}
	}
	snapshot.Account = "default"
	return snapshot, nil
}

func (c *Client) refresh(ctx context.Context, auth *Auth) error {
	requestBody, _ := json.Marshal(map[string]string{
		"client_id": clientID, "grant_type": "refresh_token", "refresh_token": auth.RefreshToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, bytes.NewReader(requestBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return &provider.Error{Kind: provider.ErrorTransport, Provider: c.ID(), Message: "Codex token refresh failed", Err: err}
	}
	defer response.Body.Close()
	body, err := readBody(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), StatusCode: response.StatusCode, Message: "Codex token refresh failed; run `codex login`"}
	}
	var refreshed struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		IDToken      string          `json:"id_token"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &refreshed); err != nil || strings.TrimSpace(refreshed.AccessToken) == "" {
		return &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "Codex token refresh returned an invalid response", Err: err}
	}
	expiresIn, _ := optionalInt(refreshed.ExpiresIn)
	expiresAt := c.now().Add(time.Duration(expiresIn) * time.Second)
	if expiresIn <= 0 {
		expiresAt = jwtExpiry(refreshed.AccessToken)
	}
	auth.Update(refreshed.AccessToken, strings.TrimSpace(refreshed.RefreshToken), strings.TrimSpace(refreshed.IDToken), expiresAt)
	if err := auth.Save(c.AuthPath); err != nil {
		return &provider.Error{Kind: provider.ErrorCredentials, Provider: c.ID(), Message: "Codex credentials could not be updated safely; run `codex login` if the problem persists", Err: err}
	}
	return nil
}

type usageResponse struct {
	PlanType             string                `json:"plan_type"`
	RateLimit            *rateLimit            `json:"rate_limit"`
	CodeReviewRateLimit  *rateLimit            `json:"code_review_rate_limit"`
	AdditionalRateLimits []additionalRateLimit `json:"additional_rate_limits"`
	Credits              *credits              `json:"credits"`
}

type additionalRateLimit struct {
	LimitName      string     `json:"limit_name"`
	MeteredFeature string     `json:"metered_feature"`
	RateLimit      *rateLimit `json:"rate_limit"`
}

type rateLimit struct {
	Primary   *window `json:"primary_window"`
	Secondary *window `json:"secondary_window"`
}

type window struct {
	UsedPercent       json.RawMessage `json:"used_percent"`
	Duration          json.RawMessage `json:"limit_window_seconds"`
	ResetAt           json.RawMessage `json:"reset_at"`
	ResetAfterSeconds json.RawMessage `json:"reset_after_seconds"`
}

type credits struct {
	Balance             json.RawMessage `json:"balance"`
	HasCredits          bool            `json:"has_credits"`
	Unlimited           bool            `json:"unlimited"`
	ApproxLocalMessages []uint64        `json:"approx_local_messages"`
	ApproxCloudMessages []uint64        `json:"approx_cloud_messages"`
}

func parseUsage(data []byte, planHint string, now time.Time) (model.Snapshot, error) {
	var response usageResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return model.Snapshot{}, err
	}
	plan := response.PlanType
	if plan == "" {
		plan = planHint
	}
	if plan == "" {
		plan = "Unknown"
	}
	plan = strings.ToUpper(plan[:1]) + plan[1:]
	snapshot := model.Snapshot{SchemaVersion: model.SchemaVersion, Provider: "codex", Plan: "ChatGPT " + plan, CollectedAt: now.UTC()}
	seen := map[string]bool{}
	if response.RateLimit != nil {
		for index, wire := range []*window{response.RateLimit.Primary, response.RateLimit.Secondary} {
			if wire == nil {
				continue
			}
			parsed, err := parseWindow(wire, now)
			if err != nil {
				return model.Snapshot{}, err
			}
			kind, label := classifyWindow(*parsed.DurationSeconds, index)
			if seen[kind] {
				return model.Snapshot{}, fmt.Errorf("duplicate %s window", kind)
			}
			seen[kind] = true
			parsed.Kind, parsed.Label = kind, label
			snapshot.Windows = append(snapshot.Windows, parsed)
		}
	}
	if response.CodeReviewRateLimit != nil && response.CodeReviewRateLimit.Primary != nil {
		parsed, err := parseWindow(response.CodeReviewRateLimit.Primary, now)
		if err != nil {
			return model.Snapshot{}, err
		}
		parsed.Kind, parsed.Label = "code_review", "Reviews"
		snapshot.Windows = append(snapshot.Windows, parsed)
	}
	for _, additional := range response.AdditionalRateLimits {
		if additional.RateLimit == nil {
			continue
		}
		name := firstNonEmpty(additional.LimitName, additional.MeteredFeature, "Additional")
		for index, wire := range []*window{additional.RateLimit.Primary, additional.RateLimit.Secondary} {
			if wire == nil {
				continue
			}
			parsed, err := parseWindow(wire, now)
			if err != nil {
				return model.Snapshot{}, err
			}
			suffix := ""
			if additional.RateLimit.Secondary != nil {
				_, period := classifyWindow(*parsed.DurationSeconds, index)
				suffix = " " + period
			}
			parsed.Kind = "additional_" + slug(name)
			if index == 1 {
				parsed.Kind += "_secondary"
			}
			parsed.Label = name + suffix
			snapshot.Windows = append(snapshot.Windows, parsed)
		}
	}
	if response.Credits != nil {
		snapshot.Credits = &model.Credits{Balance: parseBalance(response.Credits.Balance), HasCredits: response.Credits.HasCredits, Unlimited: response.Credits.Unlimited, LocalMessages: messageRange(response.Credits.ApproxLocalMessages), CloudMessages: messageRange(response.Credits.ApproxCloudMessages)}
	}
	if len(snapshot.Windows) == 0 && snapshot.Credits == nil {
		return model.Snapshot{}, fmt.Errorf("response contains no usage windows or credits")
	}
	return snapshot, nil
}

func classifyWindow(duration int64, index int) (string, string) {
	switch duration {
	case 18000:
		return "session", "5-hour"
	case 604800:
		return "weekly", "Weekly"
	default:
		return fmt.Sprintf("window_%d_%d", duration, index), durationLabel(duration)
	}
}

func durationLabel(seconds int64) string {
	if seconds%(24*60*60) == 0 {
		return fmt.Sprintf("%d-day", seconds/(24*60*60))
	}
	if seconds%(60*60) == 0 {
		return fmt.Sprintf("%d-hour", seconds/(60*60))
	}
	if seconds%60 == 0 {
		return fmt.Sprintf("%d-minute", seconds/60)
	}
	return fmt.Sprintf("%d-second", seconds)
}

func slug(value string) string {
	var result strings.Builder
	for _, char := range strings.ToLower(value) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			result.WriteRune(char)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "_") {
			result.WriteByte('_')
		}
	}
	return strings.Trim(result.String(), "_")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseWindow(wire *window, now time.Time) (model.UsageWindow, error) {
	pct, err := flexibleFloat(wire.UsedPercent)
	if err != nil || !isFinite(pct) || pct < 0 || pct > 101 {
		return model.UsageWindow{}, fmt.Errorf("invalid used percentage")
	}
	pct = math.Min(100, math.Round(pct))
	duration, err := flexibleInt(wire.Duration)
	if err != nil || duration <= 0 {
		return model.UsageWindow{}, fmt.Errorf("invalid window duration")
	}
	var reset *time.Time
	if value, ok := optionalInt(wire.ResetAt); ok {
		t := time.Unix(value, 0).UTC()
		reset = &t
	} else if value, ok := optionalInt(wire.ResetAfterSeconds); ok {
		t := now.Add(time.Duration(value) * time.Second).UTC()
		reset = &t
	}
	return model.UsageWindow{UsedPercent: &pct, DurationSeconds: &duration, ResetsAt: reset}, nil
}

func flexibleFloat(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("missing number")
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return number.Float64()
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(text), 64)
}

func flexibleInt(raw json.RawMessage) (int64, error) {
	value, err := flexibleFloat(raw)
	if err != nil || !isFinite(value) || value != math.Trunc(value) || value > math.MaxInt64 || value < math.MinInt64 {
		return 0, fmt.Errorf("invalid integer")
	}
	return int64(value), nil
}

func optionalInt(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	value, err := flexibleInt(raw)
	return value, err == nil
}

func isFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func parseBalance(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	if value, err := flexibleFloat(raw); err == nil && isFinite(value) {
		return fmt.Sprintf("$%.2f", value)
	}
	return ""
}

func messageRange(values []uint64) *[2]uint64 {
	if len(values) == 0 {
		return nil
	}
	result := [2]uint64{values[0], values[0]}
	if len(values) > 1 {
		result[1] = values[1]
	}
	return &result
}

func readBody(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, maxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodySize {
		return nil, &provider.Error{Kind: provider.ErrorSchema, Message: "provider response exceeds 2 MiB"}
	}
	return body, nil
}

package kimi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gustmrg/ai-usage/internal/provider"
)

const (
	defaultBaseURL   = "https://api.kimi.com/coding/v1"
	defaultOAuthHost = "https://auth.kimi.com"
	oauthClientID    = "17e5f671-d194-4dfb-9706-5516cb48c098"
)

type oauthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type credential struct {
	accessToken string
	cacheKey    string
	usageURL    string
	source      string
}

type oauthLocation struct {
	home        string
	storageName string
	tokenPath   string
	oauthHost   string
	baseURL     string
}

func kimiHome() (string, error) {
	if value := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME")); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kimi-code"), nil
}

func resolveOAuthLocation() (oauthLocation, error) {
	home, err := kimiHome()
	if err != nil {
		return oauthLocation{}, err
	}
	baseURL := normalizeEndpoint(os.Getenv("KIMI_CODE_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	oauthHost := normalizeEndpoint(os.Getenv("KIMI_CODE_OAUTH_HOST"))
	if oauthHost == "" {
		oauthHost = normalizeEndpoint(os.Getenv("KIMI_OAUTH_HOST"))
	}
	if oauthHost == "" {
		oauthHost = defaultOAuthHost
	}
	storageName := "kimi-code"
	if baseURL != defaultBaseURL || oauthHost != defaultOAuthHost {
		scope, _ := json.Marshal(struct {
			OAuthHost string `json:"oauthHost"`
			BaseURL   string `json:"baseUrl"`
		}{OAuthHost: oauthHost, BaseURL: baseURL})
		digest := sha256.Sum256(scope)
		storageName = "kimi-code-env-" + hex.EncodeToString(digest[:8])
	}
	return oauthLocation{
		home:        home,
		storageName: storageName,
		tokenPath:   filepath.Join(home, "credentials", storageName+".json"),
		oauthHost:   oauthHost,
		baseURL:     baseURL,
	}, nil
}

func normalizeEndpoint(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func readOAuthToken(path string) (oauthToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return oauthToken{}, err
	}
	var token oauthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return oauthToken{}, fmt.Errorf("decode Kimi OAuth credentials: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return oauthToken{}, fmt.Errorf("Kimi OAuth login is missing or revoked; run `kimi login`")
	}
	return token, nil
}

func oauthCacheKey(token oauthToken) string {
	identity := jwtSubject(token.AccessToken)
	if identity == "" {
		identity = token.RefreshToken
	}
	return provider.CacheFingerprint("oauth:" + identity)
}

func jwtSubject(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	subject, _ := claims["sub"].(string)
	return subject
}

func shouldRefresh(token oauthToken, now time.Time) bool {
	if token.ExpiresAt == 0 {
		return false
	}
	threshold := int64(300)
	if token.ExpiresIn/2 > threshold {
		threshold = token.ExpiresIn / 2
	}
	return token.ExpiresAt-now.Unix() < threshold
}

func (c *Client) resolveCredential(ctx context.Context) (credential, error) {
	location, err := resolveOAuthLocation()
	if err != nil {
		return credential{}, err
	}
	token, err := readOAuthToken(location.tokenPath)
	if err == nil {
		if shouldRefresh(token, c.now()) {
			token, err = c.refreshOAuth(ctx, location)
			if err != nil {
				return credential{}, err
			}
		}
		return credential{
			accessToken: token.AccessToken,
			cacheKey:    oauthCacheKey(token),
			usageURL:    location.baseURL + "/usages",
			source:      "Kimi Code OAuth",
		}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return credential{}, err
	}
	key := strings.TrimSpace(os.Getenv("KIMI_API_KEY"))
	if key == "" {
		return credential{}, fmt.Errorf("Kimi subscription credentials not found at %s; run `kimi login`", location.tokenPath)
	}
	return credential{
		accessToken: key,
		cacheKey:    provider.CacheFingerprint("api-key:" + key),
		usageURL:    location.baseURL + "/usages",
		source:      "KIMI_API_KEY",
	}, nil
}

func (c *Client) refreshOAuth(ctx context.Context, location oauthLocation) (oauthToken, error) {
	release, err := acquireOAuthLock(ctx, location)
	if err != nil {
		return oauthToken{}, err
	}
	defer release()

	active, err := readOAuthToken(location.tokenPath)
	if err != nil {
		return oauthToken{}, err
	}
	if !shouldRefresh(active, c.now()) {
		return active, nil
	}
	if active.RefreshToken == "" {
		return oauthToken{}, fmt.Errorf("Kimi OAuth token cannot be refreshed; run `kimi login`")
	}

	form := url.Values{
		"client_id":     {oauthClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {active.RefreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, location.oauthHost+"/api/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return oauthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return oauthToken{}, &provider.Error{Kind: provider.ErrorTransport, Provider: "kimi", Message: "Kimi OAuth refresh failed", Err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return oauthToken{}, err
	}
	if len(body) > 2<<20 {
		return oauthToken{}, fmt.Errorf("Kimi OAuth response exceeds 2 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return oauthToken{}, &provider.Error{Kind: provider.ErrorCredentials, Provider: "kimi", StatusCode: response.StatusCode, Message: "Kimi OAuth login expired; run `kimi login`"}
		}
		return oauthToken{}, &provider.Error{Kind: provider.ErrorHTTP, Provider: "kimi", StatusCode: response.StatusCode, Message: fmt.Sprintf("Kimi OAuth refresh returned HTTP %d", response.StatusCode)}
	}
	var refreshed oauthToken
	if err := json.Unmarshal(body, &refreshed); err != nil {
		return oauthToken{}, fmt.Errorf("decode Kimi OAuth refresh: %w", err)
	}
	if refreshed.AccessToken == "" || refreshed.ExpiresIn <= 0 {
		return oauthToken{}, fmt.Errorf("Kimi OAuth refresh returned an invalid token")
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = active.RefreshToken
	}
	if refreshed.Scope == "" {
		refreshed.Scope = active.Scope
	}
	if refreshed.TokenType == "" {
		refreshed.TokenType = active.TokenType
	}
	refreshed.ExpiresAt = c.now().Unix() + refreshed.ExpiresIn
	if err := writeOAuthToken(location.tokenPath, refreshed); err != nil {
		return oauthToken{}, fmt.Errorf("save refreshed Kimi OAuth token: %w", err)
	}
	return refreshed, nil
}

func writeOAuthToken(path string, token oauthToken) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".kimi-code-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func acquireOAuthLock(ctx context.Context, location oauthLocation) (func(), error) {
	if runtime.GOOS == "windows" || os.Getenv("KIMI_DISABLE_OAUTH_LOCK") == "1" {
		return func() {}, nil
	}
	oauthDir := filepath.Join(location.home, "oauth")
	if err := os.MkdirAll(oauthDir, 0o700); err != nil {
		return nil, fmt.Errorf("prepare Kimi OAuth lock: %w", err)
	}
	target := filepath.Join(oauthDir, location.storageName)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("prepare Kimi OAuth lock: %w", err)
	}
	file.Close()
	lockDir := target + ".lock"
	for {
		if err := os.Mkdir(lockDir, 0o700); err == nil {
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case now := <-ticker.C:
						_ = os.Chtimes(lockDir, now, now)
					case <-stop:
						return
					}
				}
			}()
			return func() {
				close(stop)
				<-done
				_ = os.RemoveAll(lockDir)
			}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire Kimi OAuth lock: %w", err)
		}
		if info, err := os.Stat(lockDir); err == nil && time.Since(info.ModTime()) > 5*time.Second {
			_ = os.RemoveAll(lockDir)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Kimi OAuth lock: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

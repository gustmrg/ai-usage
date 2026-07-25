package codex

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Auth struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	ExpiresAt    time.Time
	root         map[string]json.RawMessage
	tokens       map[string]json.RawMessage
	originalHash [sha256.Size]byte
}

func DefaultAuthPath() (string, error) {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Join(codexHome, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

func ReadAuth(path string) (*Auth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Codex credentials: %w", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode Codex credentials; run `codex login`: %w", err)
	}
	var tokens map[string]json.RawMessage
	if err := json.Unmarshal(root["tokens"], &tokens); err != nil {
		return nil, fmt.Errorf("decode Codex token block; run `codex login`: %w", err)
	}
	auth := &Auth{root: root, tokens: tokens, originalHash: sha256.Sum256(data)}
	if err := requiredString(tokens, "access_token", &auth.AccessToken); err != nil {
		return nil, err
	}
	if err := requiredString(tokens, "refresh_token", &auth.RefreshToken); err != nil {
		return nil, err
	}
	if err := requiredString(tokens, "id_token", &auth.IDToken); err != nil {
		return nil, err
	}
	_ = optionalString(tokens, "account_id", &auth.AccountID)
	var expires string
	if optionalString(tokens, "expires_at", &expires) == nil && expires != "" {
		auth.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	}
	if auth.ExpiresAt.IsZero() {
		auth.ExpiresAt = jwtExpiry(auth.AccessToken)
	}
	if auth.ExpiresAt.IsZero() {
		auth.ExpiresAt = jwtExpiry(auth.IDToken)
	}
	return auth, nil
}

func requiredString(values map[string]json.RawMessage, key string, destination *string) error {
	if err := optionalString(values, key, destination); err != nil || strings.TrimSpace(*destination) == "" {
		return fmt.Errorf("Codex credentials are missing %s; run `codex login`", key)
	}
	return nil
}

func optionalString(values map[string]json.RawMessage, key string, destination *string) error {
	raw, ok := values[key]
	if !ok || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, destination)
}

func jwtExpiry(token string) time.Time {
	claims := jwtClaims(token)
	value, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}
	}
	return time.Unix(int64(value), 0).UTC()
}

func planFromJWT(token string) string {
	claims := jwtClaims(token)
	authClaim, ok := claims["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	plan, _ := authClaim["chatgpt_plan_type"].(string)
	return plan
}

func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func (a *Auth) NeedsRefresh(now time.Time) bool {
	return a.ExpiresAt.IsZero() || a.ExpiresAt.Before(now.Add(5*time.Minute))
}

func (a *Auth) Update(access, refresh, idToken string, expiresAt time.Time) {
	a.AccessToken = access
	if refresh != "" {
		a.RefreshToken = refresh
	}
	if idToken != "" {
		a.IDToken = idToken
	}
	a.ExpiresAt = expiresAt
	a.tokens["access_token"], _ = json.Marshal(a.AccessToken)
	a.tokens["refresh_token"], _ = json.Marshal(a.RefreshToken)
	a.tokens["id_token"], _ = json.Marshal(a.IDToken)
	if !a.ExpiresAt.IsZero() {
		a.tokens["expires_at"], _ = json.Marshal(a.ExpiresAt.Format(time.RFC3339))
	}
}

func (a *Auth) Save(path string) error {
	tokens, err := json.Marshal(a.tokens)
	if err != nil {
		return err
	}
	a.root["tokens"] = tokens
	data, err := json.MarshalIndent(a.root, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".auth-*")
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
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if sha256.Sum256(current) != a.originalHash {
		return fmt.Errorf("Codex credentials changed during refresh; refusing to overwrite them")
	}
	return os.Rename(tmpPath, path)
}

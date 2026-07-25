package model

import (
	"encoding/json"
	"math/big"
	"time"
)

const SchemaVersion = 1

type Snapshot struct {
	SchemaVersion int             `json:"schemaVersion"`
	Provider      string          `json:"provider"`
	Account       string          `json:"account,omitempty"`
	Plan          string          `json:"plan,omitempty"`
	CollectedAt   time.Time       `json:"collectedAt"`
	Stale         bool            `json:"stale"`
	CacheAge      int64           `json:"cacheAgeSeconds,omitempty"`
	Windows       []UsageWindow   `json:"windows,omitempty"`
	Credits       *Credits        `json:"credits,omitempty"`
	ProviderData  json.RawMessage `json:"providerData,omitempty"`
}

type UsageWindow struct {
	Kind            string     `json:"kind"`
	Label           string     `json:"label"`
	Used            *uint64    `json:"used,omitempty"`
	Limit           *uint64    `json:"limit,omitempty"`
	Remaining       *uint64    `json:"remaining,omitempty"`
	UsedPercent     *float64   `json:"usedPercent,omitempty"`
	DurationSeconds *int64     `json:"durationSeconds,omitempty"`
	ResetsAt        *time.Time `json:"resetsAt,omitempty"`
}

type Credits struct {
	Balance       string     `json:"balance,omitempty"`
	HasCredits    bool       `json:"hasCredits"`
	Unlimited     bool       `json:"unlimited"`
	LocalMessages *[2]uint64 `json:"localMessages,omitempty"`
	CloudMessages *[2]uint64 `json:"cloudMessages,omitempty"`
}

type Report struct {
	SchemaVersion int             `json:"schemaVersion"`
	GeneratedAt   time.Time       `json:"generatedAt"`
	Providers     []Snapshot      `json:"providers"`
	Errors        []ProviderError `json:"errors"`
}

type ProviderError struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
}

func Percent(used, limit uint64) float64 {
	if limit == 0 {
		return 0
	}
	numerator := new(big.Int).SetUint64(used)
	numerator.Mul(numerator, big.NewInt(100))
	numerator.Add(numerator, new(big.Int).SetUint64(limit/2))
	numerator.Div(numerator, new(big.Int).SetUint64(limit))
	if numerator.Cmp(big.NewInt(100)) > 0 {
		return 100
	}
	return float64(numerator.Uint64())
}

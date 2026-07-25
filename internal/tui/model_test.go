package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gustmrg/ai-usage/internal/app"
	"github.com/gustmrg/ai-usage/internal/cache"
	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

type tuiProvider struct {
	err       error
	available bool
}

func (tuiProvider) ID() string                { return "codex" }
func (tuiProvider) DisplayName() string       { return "Codex" }
func (tuiProvider) CacheKey() (string, error) { return "account", nil }
func (p tuiProvider) Detect() provider.Detection {
	if !p.available {
		return provider.Detection{Detail: "set TEST_API_KEY"}
	}
	return provider.Detection{Available: true}
}
func (p tuiProvider) Fetch(context.Context, string) (model.Snapshot, error) {
	if p.err != nil {
		return model.Snapshot{}, p.err
	}
	pct := 51.0
	return model.Snapshot{Plan: "ChatGPT Plus", CollectedAt: time.Now().UTC(), Windows: []model.UsageWindow{{Kind: "weekly", Label: "Weekly", UsedPercent: &pct}}}, nil
}

func TestFetchTransitionsProviderToReady(t *testing.T) {
	service := app.NewService(cache.At(t.TempDir()), tuiProvider{available: true})
	m := New(service)
	command := m.fetch(0, false)
	if command == nil || !m.providers[0].loading {
		t.Fatal("fetch did not enter loading state")
	}
	message := command()
	m.Update(message)
	if m.providers[0].loading || m.providers[0].snapshot.Plan != "ChatGPT Plus" {
		t.Fatalf("state = %#v", m.providers[0])
	}
	if content := m.View().Content; !strings.Contains(content, "ChatGPT Plus") || !strings.Contains(content, "Weekly") {
		t.Fatalf("view does not contain provider data: %q", content)
	}
}

func TestStaleRequestResultIsIgnored(t *testing.T) {
	service := app.NewService(cache.At(t.TempDir()), tuiProvider{available: true})
	m := New(service)
	m.providers[0].requestID = 2
	m.Update(fetchMsg{id: "codex", requestID: 1, result: app.Result{Err: errors.New("old error")}})
	if m.providers[0].err != nil {
		t.Fatalf("stale result changed state: %v", m.providers[0].err)
	}
}

func TestUnavailableProviderStillHasSetupTab(t *testing.T) {
	service := app.NewService(cache.At(t.TempDir()), tuiProvider{})
	m := New(service)
	if len(m.providers) != 1 || m.providers[0].available {
		t.Fatalf("providers = %#v", m.providers)
	}
	content := m.View().Content
	if !strings.Contains(content, "Codex") || !strings.Contains(content, "Not configured") || !strings.Contains(content, "set TEST_API_KEY") {
		t.Fatalf("setup tab is incomplete: %q", content)
	}
}

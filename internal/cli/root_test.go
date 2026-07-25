package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gustmrg/ai-usage/internal/app"
	"github.com/gustmrg/ai-usage/internal/cache"
	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

type cliProvider struct{}

func (cliProvider) ID() string                { return "codex" }
func (cliProvider) DisplayName() string       { return "Codex" }
func (cliProvider) CacheKey() (string, error) { return "account", nil }
func (cliProvider) Detect() provider.Detection {
	return provider.Detection{Available: true, Detail: "test credentials"}
}
func (cliProvider) Fetch(context.Context, string) (model.Snapshot, error) {
	pct := 42.0
	return model.Snapshot{Plan: "ChatGPT Test", CollectedAt: time.Now().UTC(), Windows: []model.UsageWindow{{Kind: "weekly", Label: "Weekly", UsedPercent: &pct}}}, nil
}

func TestStatusJSONHasStableEnvelope(t *testing.T) {
	service := app.NewService(cache.At(t.TempDir()), cliProvider{})
	var stdout, stderr bytes.Buffer
	command := New(service, &stdout, &stderr)
	command.SetArgs([]string{"status", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var report model.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout.String())
	}
	if report.SchemaVersion != model.SchemaVersion || len(report.Providers) != 1 || report.Providers[0].Provider != "codex" {
		t.Fatalf("report = %#v", report)
	}
}

func TestStatusRejectsUnknownProvider(t *testing.T) {
	service := app.NewService(cache.At(t.TempDir()), cliProvider{})
	command := New(service, &bytes.Buffer{}, &bytes.Buffer{})
	command.SetArgs([]string{"status", "--provider", "unknown"})
	if err := command.Execute(); err == nil {
		t.Fatal("status accepted an unknown provider")
	}
}

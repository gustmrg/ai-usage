package render

import (
	"strings"
	"testing"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
)

func TestSnapshotRendersRemainingQuota(t *testing.T) {
	used := 68.0
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	output := Snapshot(model.Snapshot{
		Provider:    "kimi",
		Plan:        "Basic",
		CollectedAt: now,
		Windows: []model.UsageWindow{{
			Kind:        "weekly",
			Label:       "Weekly",
			UsedPercent: &used,
			ResetsAt:    &reset,
		}},
	}, 50, now)
	if !strings.Contains(output, "32% left") {
		t.Fatalf("output does not show remaining quota: %q", output)
	}
	wantBar := ProgressBar(32, 17)
	if !strings.Contains(output, wantBar) {
		t.Fatalf("output does not fill the remaining portion: %q", output)
	}
	if strings.Contains(output, "% left  resets in") || !strings.Contains(output, "% left\n          resets in 2h") {
		t.Fatalf("reset text is not on its own line: %q", output)
	}
}

func TestSnapshotClampsProviderPercentageBeforeInverting(t *testing.T) {
	used := 101.0
	now := time.Now()
	output := Snapshot(model.Snapshot{
		Provider:    "codex",
		CollectedAt: now,
		Windows:     []model.UsageWindow{{Label: "Weekly", UsedPercent: &used}},
	}, 50, now)
	if !strings.Contains(output, "0% left") {
		t.Fatalf("output = %q", output)
	}
}

func TestSnapshotSeparatesKimiWindowsAndHidesCodexCredits(t *testing.T) {
	used := 50.0
	now := time.Now()
	windows := []model.UsageWindow{
		{Kind: "session", Label: "5-hour", UsedPercent: &used},
		{Kind: "weekly", Label: "Weekly", UsedPercent: &used},
	}
	kimiOutput := Snapshot(model.Snapshot{Provider: "kimi", CollectedAt: now, Windows: windows}, 50, now)
	if !strings.Contains(kimiOutput, "% left\n\nWeekly") {
		t.Fatalf("Kimi windows are not separated: %q", kimiOutput)
	}
	opencodegoOutput := Snapshot(model.Snapshot{Provider: "opencodego", CollectedAt: now, Windows: windows}, 50, now)
	if !strings.Contains(opencodegoOutput, "% left\n\nWeekly") {
		t.Fatalf("OpenCode Go windows are not separated: %q", opencodegoOutput)
	}
	codexOutput := Snapshot(model.Snapshot{
		Provider:    "codex",
		CollectedAt: now,
		Windows:     windows[1:],
		Credits:     &model.Credits{Balance: "$12.00", HasCredits: true},
	}, 50, now)
	if strings.Contains(codexOutput, "Credits") || strings.Contains(codexOutput, "$12.00") {
		t.Fatalf("Codex credits are visible: %q", codexOutput)
	}
}

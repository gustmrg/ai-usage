package kimi

import (
	"math"
	"testing"
	"time"
)

func TestParseUsageSupportsStringCountsAndFiveHourAlias(t *testing.T) {
	snapshot, err := parseUsage([]byte(`{
      "user":{"membership":{"level":"LEVEL_INTERMEDIATE"}},
      "usage":{"limit":"100","used":"26","remaining":"74","resetTime":"2026-07-30T12:00:00Z"},
      "limits":[{"window":{"duration":5,"time_unit":"HOURS"},"detail":{"limit":"100","used":"15","remaining":"85","reset_at":"2026-07-24T17:00:00Z"}}]
    }`), time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Plan != "Intermediate" || len(snapshot.Windows) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Windows[0].Kind != "session" || *snapshot.Windows[0].UsedPercent != 15 {
		t.Fatalf("session window = %#v", snapshot.Windows[0])
	}
	if snapshot.Windows[1].Kind != "weekly" || *snapshot.Windows[1].UsedPercent != 26 {
		t.Fatalf("weekly window = %#v", snapshot.Windows[1])
	}
}

func TestParseUsageDerivesMissingCountAtUint64Limit(t *testing.T) {
	snapshot, err := parseUsage([]byte(`{
      "usage":{"limit":"18446744073709551615","remaining":"9223372036854775807"}
    }`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	window := snapshot.Windows[0]
	if *window.Used != uint64(math.MaxUint64)-uint64(math.MaxInt64) {
		t.Fatalf("used = %d", *window.Used)
	}
	if *window.UsedPercent != 50 {
		t.Fatalf("percent = %v, want 50", *window.UsedPercent)
	}
}

func TestParseUsagePreservesWeeklyWhenRollingWindowIsUnknown(t *testing.T) {
	snapshot, err := parseUsage([]byte(`{
      "usage":{"limit":100,"used":10},
      "limits":[{"window":{"duration":60,"timeUnit":"MINUTES"},"detail":{"limit":100,"used":10}}]
    }`), time.Now())
	if err != nil || len(snapshot.Windows) != 1 || snapshot.Windows[0].Kind != "weekly" {
		t.Fatalf("parseUsage() = %#v, %v", snapshot, err)
	}
}

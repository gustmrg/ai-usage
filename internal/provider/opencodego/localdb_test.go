package opencodego

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeWindows(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) // Saturday
	lastActivity := now.Add(-1 * time.Hour)
	samples := []usageSample{
		{at: lastActivity, costUSDCents: 600},              // $6 within the rolling window
		{at: now.Add(-48 * time.Hour), costUSDCents: 1500}, // $15 this week, outside rolling
		{at: now.AddDate(0, -1, 0), costUSDCents: 99999},   // last month, ignored everywhere
	}
	windows := computeWindows(samples, now)
	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}

	rolling := windows[0]
	if *rolling.Used != 600 || *rolling.Limit != rollingLimitCents {
		t.Errorf("rolling used/limit = %d/%d, want 600/%d", *rolling.Used, *rolling.Limit, rollingLimitCents)
	}
	if *rolling.UsedPercent != 50 {
		t.Errorf("rolling percent = %v, want 50", *rolling.UsedPercent)
	}
	if want := lastActivity.Add(5 * time.Hour); !rolling.ResetsAt.Equal(want) {
		t.Errorf("rolling reset = %v, want %v", rolling.ResetsAt, want)
	}

	weekly := windows[1]
	if *weekly.Used != 2100 {
		t.Errorf("weekly used = %d, want 2100", *weekly.Used)
	}
	if *weekly.UsedPercent != 70 {
		t.Errorf("weekly percent = %v, want 70", *weekly.UsedPercent)
	}
	if want := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC); !weekly.ResetsAt.Equal(want) {
		t.Errorf("weekly reset = %v, want %v (Monday 00:00 UTC)", weekly.ResetsAt, want)
	}

	monthly := windows[2]
	if *monthly.Used != 2100 {
		t.Errorf("monthly used = %d, want 2100", *monthly.Used)
	}
	if *monthly.UsedPercent != 35 {
		t.Errorf("monthly percent = %v, want 35", *monthly.UsedPercent)
	}
	if want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC); !monthly.ResetsAt.Equal(want) {
		t.Errorf("monthly reset = %v, want %v", monthly.ResetsAt, want)
	}
	if monthly.DurationSeconds != nil {
		t.Errorf("monthly duration should be nil, got %v", *monthly.DurationSeconds)
	}
}

func TestComputeWindowsIdle(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	samples := []usageSample{
		{at: now.Add(-45 * 24 * time.Hour), costUSDCents: 500}, // stale, outside every window
	}
	windows := computeWindows(samples, now)
	rolling := windows[0]
	if *rolling.Used != 0 || *rolling.UsedPercent != 0 {
		t.Errorf("rolling should be idle, got used=%d pct=%v", *rolling.Used, *rolling.UsedPercent)
	}
	if want := now.Add(5 * time.Hour); !rolling.ResetsAt.Equal(want) {
		t.Errorf("idle rolling reset = %v, want %v", rolling.ResetsAt, want)
	}
	if *windows[1].Used != 0 || *windows[2].Used != 0 {
		t.Errorf("weekly/monthly should exclude stale samples")
	}
}

func TestWeekBounds(t *testing.T) {
	cases := []struct {
		now, start time.Time
	}{
		{time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)},  // Saturday
		{time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)},   // Monday midnight
		{time.Date(2026, 7, 26, 23, 59, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}, // Sunday
	}
	for _, tc := range cases {
		start, end := weekBounds(tc.now)
		if !start.Equal(tc.start) || !end.Equal(tc.start.AddDate(0, 0, 7)) {
			t.Errorf("weekBounds(%v) = %v..%v, want %v..%v", tc.now, start, end, tc.start, tc.start.AddDate(0, 0, 7))
		}
	}
}

func TestReadUsageSamples(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE message (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		time_created INTEGER NOT NULL,
		time_updated INTEGER NOT NULL,
		data TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	now := time.Now().UnixMilli()
	rows := []struct {
		id   string
		at   int64
		data string
	}{
		{"m1", now, `{"role":"assistant","providerID":"opencode-go","modelID":"glm-5.2","cost":0.25}`},
		{"m2", now, `{"role":"assistant","providerID":"opencode-go","modelID":"kimi-k3","cost":1.5}`},
		{"m3", now, `{"role":"assistant","providerID":"opencode","modelID":"gpt-5.5","cost":9.99}`}, // Zen, not Go
		{"m4", now, `{"role":"user","providerID":"opencode-go"}`},
		{"m5", now - 200*24*3600*1000, `{"role":"assistant","providerID":"opencode-go","cost":5.0}`}, // too old
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, 's1', ?, ?, ?)`,
			row.id, row.at, row.at, row.data); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}
	db.Close()

	samples, err := readUsageSamples(dbPath, time.Now().AddDate(0, -2, 0))
	if err != nil {
		t.Fatalf("readUsageSamples: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}
	total := samples[0].costUSDCents + samples[1].costUSDCents
	if fmt.Sprintf("%.2f", total) != "175.00" {
		t.Errorf("total cents = %v, want 175", total)
	}
}

func TestDetectWithoutCredentials(t *testing.T) {
	t.Setenv("OPENCODE_SESSION_COOKIE", "")
	t.Setenv("OPENCODE_DATA_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	client := New(nil)
	if detection := client.Detect(); detection.Available {
		t.Error("expected provider to be unavailable without cookie or database")
	}
}

func TestDetectWithCookie(t *testing.T) {
	t.Setenv("OPENCODE_SESSION_COOKIE", "session=abc")
	client := New(nil)
	if detection := client.Detect(); !detection.Available {
		t.Error("expected provider to be available with cookie")
	}
}

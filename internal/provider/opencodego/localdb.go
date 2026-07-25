package opencodego

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/provider"
)

// defaultDBPath locates opencode's local SQLite database.
func defaultDBPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("OPENCODE_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "opencode.db"), nil
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "opencode", "opencode.db"), nil
	}
	if runtime.GOOS == "windows" {
		if dir := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); dir != "" {
			return filepath.Join(dir, "opencode", "opencode.db"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db"), nil
}

// usageSample is one billable opencode-go assistant message.
type usageSample struct {
	at           time.Time
	costUSDCents float64
}

func (c *Client) fetchLocal(dbPath string) (model.Snapshot, error) {
	samples, err := readUsageSamples(dbPath, c.now().AddDate(0, -2, 0))
	if err != nil {
		return model.Snapshot{}, &provider.Error{Kind: provider.ErrorSchema, Provider: c.ID(), Message: "read local opencode database", Err: err}
	}
	return newSnapshot(c.now(), computeWindows(samples, c.now())), nil
}

// readUsageSamples sums per-message costs recorded locally by opencode for
// the opencode-go provider. Message timestamps are Unix milliseconds.
func readUsageSamples(dbPath string, since time.Time) ([]usageSample, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT time_created, json_extract(data, '$.cost')
		 FROM message
		 WHERE json_extract(data, '$.role') = 'assistant'
		   AND json_extract(data, '$.providerID') = 'opencode-go'
		   AND json_extract(data, '$.cost') IS NOT NULL
		   AND time_created >= ?`, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	samples := []usageSample{}
	for rows.Next() {
		var millis int64
		var cost float64
		if err := rows.Scan(&millis, &cost); err != nil {
			return nil, err
		}
		samples = append(samples, usageSample{at: time.UnixMilli(millis), costUSDCents: cost * 100})
	}
	return samples, rows.Err()
}

// computeWindows approximates the server-side Go limits from local history:
//   - rolling: the server anchors the 5-hour window on the last usage
//     update, so usage sums the 5 hours preceding the latest sample and
//     resets 5 hours after it
//   - weekly: Monday 00:00 UTC, matching the console's getWeekBounds
//   - monthly: the server anchors on the subscription date, which is not
//     knowable locally, so this uses the UTC calendar month instead
func computeWindows(samples []usageSample, now time.Time) []model.UsageWindow {
	now = now.UTC()

	var lastActivity time.Time
	for _, sample := range samples {
		if sample.at.After(lastActivity) {
			lastActivity = sample.at
		}
	}

	rollingUsed := uint64(0)
	rollingReset := now.Add(time.Duration(rollingWindowSeconds) * time.Second)
	if !lastActivity.IsZero() && now.Sub(lastActivity) < time.Duration(rollingWindowSeconds)*time.Second {
		windowStart := lastActivity.Add(-time.Duration(rollingWindowSeconds) * time.Second)
		rollingUsed = centsInWindow(samples, windowStart, lastActivity)
		rollingReset = lastActivity.Add(time.Duration(rollingWindowSeconds) * time.Second)
	}

	weekStart, weekEnd := weekBounds(now)
	weeklyUsed := centsInWindow(samples, weekStart, weekEnd)

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	monthlyUsed := centsInWindow(samples, monthStart, monthEnd)

	return []model.UsageWindow{
		percentWindow("session", "5-hour", rollingUsed, rollingLimitCents, rollingWindowSeconds, rollingReset),
		percentWindow("weekly", "Weekly", weeklyUsed, weeklyLimitCents, weeklyWindowSeconds, weekEnd),
		percentWindow("monthly", "Monthly", monthlyUsed, monthlyLimitCents, 0, monthEnd),
	}
}

func centsInWindow(samples []usageSample, start, end time.Time) uint64 {
	total := 0.0
	for _, sample := range samples {
		if !sample.at.Before(start) && !sample.at.After(end) {
			total += sample.costUSDCents
		}
	}
	return uint64(math.Round(total))
}

// weekBounds mirrors getWeekBounds in opencode's console: weeks start
// Monday 00:00 UTC.
func weekBounds(now time.Time) (time.Time, time.Time) {
	offset := (int(now.Weekday()) + 6) % 7
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := midnight.AddDate(0, 0, -offset)
	return start, start.AddDate(0, 0, 7)
}

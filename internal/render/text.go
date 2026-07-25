package render

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gustmrg/ai-usage/internal/model"
)

func Snapshot(snapshot model.Snapshot, width int, now time.Time) string {
	if width < 12 {
		width = 12
	}
	var output strings.Builder
	title := snapshot.Plan
	if title == "" {
		title = strings.ToUpper(snapshot.Provider[:1]) + snapshot.Provider[1:]
	}
	fmt.Fprintln(&output, title)
	fmt.Fprintln(&output)
	for index, window := range snapshot.Windows {
		if snapshot.Provider == "kimi" && index > 0 {
			fmt.Fprintln(&output)
		}
		remainingPct := 0.0
		pctText := "n/a"
		if window.UsedPercent != nil {
			remainingPct = 100 - math.Max(0, math.Min(100, *window.UsedPercent))
			pctText = fmt.Sprintf("%3.0f%% left", remainingPct)
		}
		barWidth := width - len(window.Label) - len(pctText) - 18
		if barWidth < 8 {
			barWidth = 8
		}
		fmt.Fprintf(&output, "%-9s %s  %s", window.Label, ProgressBar(remainingPct, barWidth), pctText)
		fmt.Fprintln(&output)
		if window.ResetsAt != nil {
			fmt.Fprintf(&output, "%-9s %s\n", "", ResetText(*window.ResetsAt, now))
		}
	}
	age := now.Sub(snapshot.CollectedAt)
	if age < 0 {
		age = 0
	}
	state := "Updated"
	if snapshot.Stale {
		state = "Stale"
	}
	fmt.Fprintf(&output, "\n%s %s ago", state, ShortDuration(age))
	return strings.TrimRight(output.String(), "\n")
}

func ProgressBar(percent float64, width int) string {
	percent = math.Max(0, math.Min(100, percent))
	filled := int(math.Round(percent / 100 * float64(width)))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func ResetText(reset, now time.Time) string {
	remaining := reset.Sub(now)
	if remaining <= 0 {
		return "reset due"
	}
	return "resets in " + ShortDuration(remaining)
}

func ShortDuration(duration time.Duration) string {
	if duration < time.Minute {
		seconds := int(duration.Round(time.Second).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		return fmt.Sprintf("%ds", seconds)
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	if hours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, hours)
}

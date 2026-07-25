package model

import (
	"math"
	"testing"
)

func TestPercentHandlesLargeCountersAndClamps(t *testing.T) {
	if got := Percent(math.MaxUint64/2, math.MaxUint64); got != 50 {
		t.Fatalf("Percent() = %v, want 50", got)
	}
	if got := Percent(math.MaxUint64, 1); got != 100 {
		t.Fatalf("Percent() = %v, want 100", got)
	}
	if got := Percent(10, 0); got != 0 {
		t.Fatalf("Percent() = %v, want 0", got)
	}
}

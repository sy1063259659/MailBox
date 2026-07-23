package store

import (
	"testing"
	"time"
)

func TestICloudHMEAutomationCooldownSchedule(t *testing.T) {
	want := []time.Duration{
		10 * time.Minute,
		15 * time.Minute,
		20 * time.Minute,
		30 * time.Minute,
		45 * time.Minute,
		60 * time.Minute,
	}
	for index, expected := range want {
		if got := iCloudHMEAutomationCooldownDelay(index + 1); got != expected {
			t.Fatalf("level %d delay = %v, want %v", index+1, got, expected)
		}
	}
	if got := iCloudHMEAutomationCooldownDelay(99); got != time.Hour {
		t.Fatalf("capped delay = %v, want 1h", got)
	}
}

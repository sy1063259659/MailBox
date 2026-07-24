package store

import (
	"testing"
	"time"
)

func TestICloudHMEAutomationCooldownSchedule(t *testing.T) {
	want := []time.Duration{
		5 * time.Minute,
		8 * time.Minute,
		12 * time.Minute,
		20 * time.Minute,
		30 * time.Minute,
		45 * time.Minute,
	}
	for index, expected := range want {
		if got := iCloudHMEAutomationCooldownDelay(index + 1); got != expected {
			t.Fatalf("level %d delay = %v, want %v", index+1, got, expected)
		}
	}
	if got := iCloudHMEAutomationCooldownDelay(99); got != 45*time.Minute {
		t.Fatalf("capped delay = %v, want 45m", got)
	}
}

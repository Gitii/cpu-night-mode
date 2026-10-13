package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testConfig(t *testing.T, text string) (config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return loadConfig(p)
}

func TestScheduleBoundariesAndTimezone(t *testing.T) {
	c, err := testConfig(t, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		instant string
		active  bool
	}{
		{"2026-01-01T17:59:59Z", false},
		{"2026-01-01T18:00:00Z", true},
		{"2026-01-02T07:59:59Z", true},
		{"2026-01-02T08:00:00Z", false},
		{"2026-07-01T16:59:59Z", false},
		{"2026-07-01T17:00:00Z", true},
		// Both occurrences of the repeated hour are inside the night window.
		{"2026-10-25T00:30:00Z", true},
		{"2026-10-25T01:30:00Z", true},
		// Spring clock jump never leaves the overnight window.
		{"2026-03-29T00:59:59Z", true},
		{"2026-03-29T01:00:00Z", true},
	} {
		now, err := time.Parse(time.RFC3339, tt.instant)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.active(now); got != tt.active {
			t.Errorf("%s: got %t, want %t", tt.instant, got, tt.active)
		}
	}
	c, err = testConfig(t, "[schedule]\nstart=10:00\nend=12:00\ntimezone=UTC\n")
	if err != nil {
		t.Fatal(err)
	}
	for hour := 0; hour < 24; hour++ {
		if got := c.active(time.Date(2026, 1, 1, hour, 0, 0, 0, time.UTC)); got != (hour >= 10 && hour < 12) {
			t.Errorf("same-day hour %d", hour)
		}
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, text := range []string{
		"[schedule]\nstart=9:00", "[schedule]\nstart=24:00", "[schedule]\nstart=09:00\nend=09:00",
		"[schedule]\ntimezone=Missing/Zone", "[schedule]\ntimezone=Local", "[schedule]\ntimezone=",
		"[cpu]\nmax_frequency_mhz=0", "[cpu]\nmax_frequency_mhz=-1", "[cpu]\nmax_frequency_mhz=1600MHz",
		"[cpu]\nmax_frequency_mhz=1600\nmax_frequency_mhz=2000", "[cpu]\nmax_frequncy_mhz=1600", "[other]", "oops",
	} {
		if _, err := testConfig(t, text); err == nil {
			t.Errorf("accepted invalid config %q", text)
		}
	}
}

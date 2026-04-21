package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseLogEntry(t *testing.T) {
	tests := []struct {
		line      string
		wantEntry *LogEntry
	}{
		{
			line: "2026/04/20 13:11:05 INFO rule state enabled=true",
			wantEntry: &LogEntry{
				Enabled: true,
			},
		},
		{
			line: "2026/04/19 10:00:00 INFO rule state enabled=false",
			wantEntry: &LogEntry{
				Enabled: false,
			},
		},
		{
			line:      "2026/04/20 10:02:00 DEBUG pushMetrics metrics=...",
			wantEntry: nil,
		},
		{
			line:      "invalid log line",
			wantEntry: nil,
		},
	}

	for _, tt := range tests {
		name := tt.line
		if len(name) > 30 {
			name = name[:30]
		}
		t.Run(name, func(t *testing.T) {
			got := parseLogEntry(tt.line)
			if (got == nil) != (tt.wantEntry == nil) {
				t.Errorf("parseLogEntry() returned nil=%v, want nil=%v", got == nil, tt.wantEntry == nil)
				return
			}
			if got != nil && got.Enabled != tt.wantEntry.Enabled {
				t.Errorf("parseLogEntry() enabled = %v, want %v", got.Enabled, tt.wantEntry.Enabled)
			}
		})
	}
}

func TestBlockedAnalysis(t *testing.T) {
	// Test data: 15 samples across 3 days
	testLines := []string{
		"2026/04/19 10:00:00 INFO rule state enabled=true",
		"2026/04/19 10:01:00 INFO rule state enabled=true",
		"2026/04/19 10:02:00 INFO rule state enabled=false",
		"2026/04/19 10:03:00 INFO rule state enabled=false",
		"2026/04/19 10:04:00 INFO rule state enabled=false",
		"2026/04/20 09:00:00 INFO rule state enabled=true",
		"2026/04/20 09:01:00 INFO rule state enabled=true",
		"2026/04/20 09:02:00 INFO rule state enabled=true",
		"2026/04/20 09:03:00 INFO rule state enabled=false",
		"2026/04/20 09:04:00 INFO rule state enabled=false",
		"2026/04/21 08:00:00 INFO rule state enabled=true",
		"2026/04/21 08:01:00 INFO rule state enabled=true",
		"2026/04/21 08:02:00 INFO rule state enabled=true",
		"2026/04/21 08:03:00 INFO rule state enabled=true",
		"2026/04/21 08:04:00 INFO rule state enabled=true",
	}

	logContent := strings.Join(testLines, "\n")
	reader := strings.NewReader(logContent)

	// Expected results based on test data:
	// 2026-04-19: 2 enabled out of 5 = 40%
	// 2026-04-20: 3 enabled out of 5 = 60%
	// 2026-04-21: 5 enabled out of 5 = 100%
	expectedResults := map[string]struct {
		total   int
		enabled int
	}{
		"2026-04-19": {total: 5, enabled: 2},
		"2026-04-20": {total: 5, enabled: 3},
		"2026-04-21": {total: 5, enabled: 5},
	}

	states, err := analyzeLog(reader, time.Time{}, "2006-01-02")
	if err != nil {
		t.Fatalf("analyzeLog failed: %v", err)
	}

	// Verify results
	for dateKey, expected := range expectedResults {
		entries, ok := states[dateKey]
		if !ok {
			t.Errorf("Missing data for date %s", dateKey)
			continue
		}

		if len(entries) != expected.total {
			t.Errorf("Date %s: got %d samples, want %d", dateKey, len(entries), expected.total)
		}

		enabledCount := 0
		for _, enabled := range entries {
			if enabled {
				enabledCount++
			}
		}

		if enabledCount != expected.enabled {
			t.Errorf("Date %s: got %d enabled, want %d", dateKey, enabledCount, expected.enabled)
		}

		pct := float64(enabledCount) / float64(len(entries)) * 100
		t.Logf("Date %s: %.1f%% enabled (%d/%d)", dateKey, pct, enabledCount, len(entries))
	}
}

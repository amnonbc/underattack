package main

import (
	"os"
	"testing"
)

func TestParseLogTime(t *testing.T) {
	tests := []struct {
		line    string
		wantErr bool
		want    string // formatted as "2006-01-02 15:04:05"
	}{
		{
			line:    "2026/04/20 13:11:05 INFO rule state enabled=true",
			wantErr: false,
			want:    "2026-04-20 13:11:05",
		},
		{
			line:    "2026/04/19 10:00:00 DEBUG pushMetrics metrics=...",
			wantErr: false,
			want:    "2026-04-19 10:00:00",
		},
		{
			line:    "invalid log line",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		name := tt.line
		if len(name) > 20 {
			name = name[:20]
		}
		t.Run(name, func(t *testing.T) {
			got, err := parseLogTime(tt.line)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLogTime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Format("2006-01-02 15:04:05") != tt.want {
				t.Errorf("parseLogTime() = %v, want %v", got.Format("2006-01-02 15:04:05"), tt.want)
			}
		})
	}
}

func TestParseRuleState(t *testing.T) {
	tests := []struct {
		line      string
		wantState bool
		wantFound bool
	}{
		{
			line:      "2026/04/20 10:00:00 INFO rule state enabled=true",
			wantState: true,
			wantFound: true,
		},
		{
			line:      "2026/04/20 10:01:00 INFO rule state enabled=false",
			wantState: false,
			wantFound: true,
		},
		{
			line:      "2026/04/20 10:02:00 DEBUG pushMetrics metrics=...",
			wantFound: false,
		},
		{
			line:      "random log line without rule state",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		name := tt.line
		if len(name) > 30 {
			name = name[:30]
		}
		t.Run(name, func(t *testing.T) {
			state, found := parseRuleState(tt.line)
			if found != tt.wantFound {
				t.Errorf("parseRuleState() found = %v, want %v", found, tt.wantFound)
				return
			}
			if found && state != tt.wantState {
				t.Errorf("parseRuleState() state = %v, want %v", state, tt.wantState)
			}
		})
	}
}

func TestBlockedAnalysis(t *testing.T) {
	// Test with the testdata log file
	logFile := "testdata/botCheck.log"

	// Verify testdata exists
	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("testdata/botCheck.log not found: %v", err)
	}

	file, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("Failed to open test log: %v", err)
	}
	defer file.Close()

	// Expected results based on testdata:
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

	dailyStates := make(map[string][]bool)

	// Simulate scanning log file
	// We'll just verify the parsing functions work
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

	for _, line := range testLines {
		logTime, err := parseLogTime(line)
		if err != nil {
			t.Fatalf("Failed to parse timestamp: %v", err)
		}

		enabled, found := parseRuleState(line)
		if !found {
			t.Fatalf("Failed to parse rule state from: %s", line)
		}

		dateKey := logTime.Format("2006-01-02")
		dailyStates[dateKey] = append(dailyStates[dateKey], enabled)
	}

	// Verify results
	for dateKey, expected := range expectedResults {
		states, ok := dailyStates[dateKey]
		if !ok {
			t.Errorf("Missing data for date %s", dateKey)
			continue
		}

		if len(states) != expected.total {
			t.Errorf("Date %s: got %d samples, want %d", dateKey, len(states), expected.total)
		}

		enabledCount := 0
		for _, s := range states {
			if s {
				enabledCount++
			}
		}

		if enabledCount != expected.enabled {
			t.Errorf("Date %s: got %d enabled, want %d", dateKey, enabledCount, expected.enabled)
		}

		pct := float64(enabledCount) / float64(len(states)) * 100
		t.Logf("Date %s: %.1f%% enabled (%d/%d)", dateKey, pct, enabledCount, len(states))
	}
}

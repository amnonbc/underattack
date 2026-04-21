package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// parseLogTime extracts the timestamp from a log line
// Format: "2026/04/20 13:11:05"
func parseLogTime(line string) (time.Time, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("no timestamp")
	}
	return time.Parse("2006/01/02 15:04:05", parts[0]+" "+parts[1])
}

// parseRuleState extracts enabled/disabled from log line
// Look for: rule state enabled=true/false
func parseRuleState(line string) (bool, bool) {
	re := regexp.MustCompile(`rule state.*enabled=(true|false)`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return matches[1] == "true", true
	}
	return false, false
}

func main() {
	days := flag.Int("days", 0, "Number of days to analyze (0 = entire file)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: blocked [-days N] logfile\n")
		os.Exit(1)
	}

	logPath := args[0]
	file, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	now := time.Now()
	var cutoff time.Time
	if *days > 0 {
		cutoff = now.AddDate(0, 0, -*days)
	} else {
		cutoff = time.Time{} // Process entire file (year 0 is before any real date)
	}

	// Map of date -> list of rule states (bool) by minute
	dailyStates := make(map[string][]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		logTime, err := parseLogTime(line)
		if err != nil {
			continue
		}

		// Skip old logs
		if logTime.Before(cutoff) {
			continue
		}

		enabled, found := parseRuleState(line)
		if !found {
			continue
		}

		// Group by date (YYYY-MM-DD)
		dateKey := logTime.Format("2006-01-02")
		dailyStates[dateKey] = append(dailyStates[dateKey], enabled)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading log file: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Println("Date           | % Enabled | Samples")
	fmt.Println("---------------|-----------|--------")

	// Sort and print by date
	for d := cutoff; !d.After(now); d = d.AddDate(0, 0, 1) {
		dateKey := d.Format("2006-01-02")
		states, ok := dailyStates[dateKey]
		if !ok || len(states) == 0 {
			continue
		}

		enabledCount := 0
		for _, s := range states {
			if s {
				enabledCount++
			}
		}

		pct := float64(enabledCount) / float64(len(states)) * 100
		fmt.Printf("%s | %8.1f%% | %d\n", dateKey, pct, len(states))
	}
}

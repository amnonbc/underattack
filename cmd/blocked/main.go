package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

type LogEntry struct {
	Time    time.Time
	Enabled bool
}

var ruleStateRe = regexp.MustCompile(`rule state.*enabled=(true|false)`)

// parseLogEntry parses a log line and returns a LogEntry if it contains a rule state,
// otherwise returns nil. The line format must be: "YYYY/MM/DD HH:MM:SS ... rule state enabled=true/false ..."
func parseLogEntry(line string) *LogEntry {
	// Extract timestamp (first two space-separated fields)
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}

	logTime, err := time.Parse("2006/01/02 15:04:05", parts[0]+" "+parts[1])
	if err != nil {
		return nil
	}

	// Extract rule state from the line
	matches := ruleStateRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil
	}

	return &LogEntry{
		Time:    logTime,
		Enabled: matches[1] == "true",
	}
}

// analyzeLog reads from the provided reader and returns rule state statistics grouped by the given timeFormat.
func analyzeLog(r io.Reader, cutoff time.Time, timeFormat string) (map[string][]bool, error) {
	states := make(map[string][]bool)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		entry := parseLogEntry(scanner.Text())
		if entry == nil {
			continue
		}

		if entry.Time.Before(cutoff) {
			continue
		}

		key := entry.Time.Format(timeFormat)
		states[key] = append(states[key], entry.Enabled)
	}

	return states, scanner.Err()
}

// reportResults prints percentage statistics grouped by timeFormat.
func reportResults(states map[string][]bool, cutoff, now time.Time, timeFormat string, increment func(time.Time) time.Time) {
	fmt.Println("Timestamp      | % Enabled | Samples")
	fmt.Println("---------------|-----------|--------")

	// Extract and sort keys to avoid iterating through all possible time periods
	keys := slices.Collect(maps.Keys(states))
	slices.Sort(keys)

	for _, key := range keys {
		entries := states[key]
		if len(entries) == 0 {
			continue
		}

		enabledCount := 0
		for _, enabled := range entries {
			if enabled {
				enabledCount++
			}
		}

		pct := float64(enabledCount) / float64(len(entries)) * 100
		fmt.Printf("%s | %8.1f%% | %d\n", key, pct, len(entries))
	}
}

func main() {
	days := flag.Int("days", 0, "Number of days to analyze (0 = entire file)")
	hours := flag.Bool("hours", false, "Summarize by hours instead of days")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: blocked [-days N] [-hours] logfile\n")
		os.Exit(1)
	}

	file, err := os.Open(args[0])
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
		cutoff = time.Time{} // Process entire file
	}

	// Choose format and increment function based on -hours flag
	var timeFormat string
	var increment func(time.Time) time.Time

	if *hours {
		timeFormat = "2006-01-02 15:00"
		increment = func(t time.Time) time.Time {
			return t.Add(time.Hour)
		}
	} else {
		timeFormat = "2006-01-02"
		increment = func(t time.Time) time.Time {
			return t.AddDate(0, 0, 1)
		}
	}

	states, err := analyzeLog(file, cutoff, timeFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing log: %v\n", err)
		os.Exit(1)
	}

	reportResults(states, cutoff, now, timeFormat, increment)
}

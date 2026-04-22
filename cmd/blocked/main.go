package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
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

// analyzeLog reads from the provided reader and calls report for each time period's statistics.
// Exploits monotonic log ordering: all entries for a given period are consecutive.
func analyzeLog(r io.Reader, cutoff time.Time, timeFormat string, report func(key string, enabledCount, total int)) error {
	scanner := bufio.NewScanner(r)

	var currentKey string
	var enabledCount, total int

	for scanner.Scan() {
		entry := parseLogEntry(scanner.Text())
		if entry == nil {
			continue
		}

		if entry.Time.Before(cutoff) {
			continue
		}

		key := entry.Time.Format(timeFormat)

		// When key changes, report previous period
		if key != currentKey && total > 0 {
			report(currentKey, enabledCount, total)
			enabledCount, total = 0, 0
		}

		currentKey = key
		total++
		if entry.Enabled {
			enabledCount++
		}
	}

	// Report final period
	if total > 0 {
		report(currentKey, enabledCount, total)
	}

	return scanner.Err()
}

// printResult is a callback that prints a single time period's statistics.
func printResult(key string, enabledCount, total int) {
	pct := float64(enabledCount) / float64(total) * 100
	fmt.Printf("%-16s | %8.1f%% | %8s | %d\n", key, pct, time.Duration(enabledCount)*time.Minute, total)
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

	// Choose format based on -hours flag
	var timeFormat string
	if *hours {
		timeFormat = "2006-01-02 15:00"
	} else {
		timeFormat = "2006-01-02"
	}

	fmt.Println("Timestamp        | % Enabled | Blocked  | Samples")
	fmt.Println("-----------------+-----------+----------+---------")

	err = analyzeLog(file, cutoff, timeFormat, printResult)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing log: %v\n", err)
		os.Exit(1)
	}
}

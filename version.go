package main

import (
	"log/slog"
	"runtime/debug"
)

// printVersion prints version info on startup.
func printVersion() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		slog.Info("version unknown")
	}
	var revision, when string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	slog.Debug("version", "sha", revision[:12], "when", when, "dirty", dirty)
}

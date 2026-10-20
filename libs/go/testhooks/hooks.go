// Package testhooks provides process-local crash barriers for P4.5 recovery tests.
// Active only when GATEFLOW_TEST_HOOKS=1. Never enables via public APIs.
package testhooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SignalReached records that a crash boundary was hit.
func SignalReached(boundary string) {
	if os.Getenv("GATEFLOW_TEST_HOOKS") != "1" {
		return
	}
	dir := os.Getenv("GATEFLOW_HOOK_DIR")
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "reached_"+boundary), []byte(boundary), 0o644)
}

// MaybeCrash exits the process if GATEFLOW_CRASH_BOUNDARY matches.
func MaybeCrash(boundary string) {
	MaybeCrashOn(boundary, "")
}

// MaybeCrashOn is MaybeCrash with an optional event/entity id filter.
// When GATEFLOW_CRASH_EVENT_FILTER is set, crash only if eventID contains that substring.
func MaybeCrashOn(boundary, eventID string) {
	if os.Getenv("GATEFLOW_TEST_HOOKS") != "1" {
		return
	}
	if os.Getenv("GATEFLOW_CRASH_BOUNDARY") != boundary {
		return
	}
	if filter := os.Getenv("GATEFLOW_CRASH_EVENT_FILTER"); filter != "" {
		if eventID == "" || !strings.Contains(eventID, filter) {
			return
		}
	}
	SignalReached(boundary)
	if ms := os.Getenv("GATEFLOW_CRASH_DELAY_MS"); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil && n > 0 {
			time.Sleep(time.Duration(n) * time.Millisecond)
		}
	}
	if os.Getenv("GATEFLOW_HOLD_AFTER_SIGNAL") == "1" {
		dir := os.Getenv("GATEFLOW_HOOK_DIR")
		start := time.Now()
		for time.Since(start) < 30*time.Second {
			if _, err := os.Stat(filepath.Join(dir, "release_"+boundary)); err == nil {
				break
			}
			if time.Since(start) > 200*time.Millisecond {
				os.Exit(99)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	os.Exit(99)
}

// Package grantprotocol selects Admission grant implementation (P6.6).
// Default is Protocol A. Invalid/missing values resolve to A (fail-safe).
package grantprotocol

import (
	"os"
	"strings"
)

const (
	EnvVar = "GATEFLOW_ADMISSION_GRANT_PROTOCOL"
	A      = "A"
	B      = "B"
)

// Parse returns A or B. Empty/missing/invalid → A.
func Parse(raw string) string {
	v := strings.ToUpper(strings.TrimSpace(raw))
	switch v {
	case "", A, "PROTOCOL_A", "FIFO", "DEFAULT":
		return A
	case B, "PROTOCOL_B", "LATE", "GENERATION":
		return B
	default:
		// Invalid → safe default A (documented; do not crash process).
		return A
	}
}

// FromEnv reads GATEFLOW_ADMISSION_GRANT_PROTOCOL once (caller should cache at startup).
func FromEnv() string {
	return Parse(os.Getenv(EnvVar))
}

// IsValid reports whether raw is an explicitly recognized value (including empty→A).
func IsValid(raw string) bool {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return true
	}
	switch v {
	case A, B, "PROTOCOL_A", "PROTOCOL_B", "FIFO", "DEFAULT", "LATE", "GENERATION":
		return true
	default:
		return false
	}
}

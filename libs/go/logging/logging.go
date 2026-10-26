package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// New returns a JSON slog logger to stdout, with optional dual-write to
// GATEFLOW_LOG_DIR/<service>.log for local Promtail → Loki shipping (P5.4.2).
// File write failures never block; stdout always remains the primary sink.
func New(service, level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	out := io.Writer(os.Stdout)
	if dir := strings.TrimSpace(os.Getenv("GATEFLOW_LOG_DIR")); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			path := filepath.Join(dir, sanitize(service)+".log")
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
				out = io.MultiWriter(os.Stdout, f)
			}
		}
	}
	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "local"
	}
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler).With("service", service, "environment", env)
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
	if s == "" {
		return "app"
	}
	return s
}

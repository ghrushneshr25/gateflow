package telemetry

import (
	"log/slog"
	"os"
	"testing"
)

func TestInitDisabledIsNoop(t *testing.T) {
	_ = os.Setenv("GATEFLOW_OTEL_ENABLED", "0")
	t.Cleanup(func() { _ = os.Unsetenv("GATEFLOW_OTEL_ENABLED") })
	shutdown := Init(slog.Default(), "test-svc")
	shutdown()
}

func TestEnabledParsing(t *testing.T) {
	_ = os.Setenv("GATEFLOW_OTEL_ENABLED", "true")
	t.Cleanup(func() { _ = os.Unsetenv("GATEFLOW_OTEL_ENABLED") })
	if !Enabled() {
		t.Fatal("expected enabled")
	}
}

package telemetry

import "log/slog"

// Init is a Phase-1 placeholder for OpenTelemetry setup.
// Full OTel wiring lands with the observability milestone.
func Init(log *slog.Logger, service string) func() {
	log.Info("telemetry placeholder initialized", "service", service)
	return func() {}
}

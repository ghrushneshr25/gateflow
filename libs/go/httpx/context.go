package httpx

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"go.opentelemetry.io/otel/trace"
)

type ctxKey int

const (
	corrKey ctxKey = 1
)

// Crockford Base32 ULID is 26 chars.
var ulidRE = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// ValidCorrelationID accepts bare ULIDs (case-insensitive) only — rejects injection/noise.
func ValidCorrelationID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return false
	}
	return ulidRE.MatchString(strings.ToUpper(s))
}

// NormalizeCorrelationID returns a valid correlation id from header or generates one.
func NormalizeCorrelationID(incoming string) string {
	if ValidCorrelationID(incoming) {
		return strings.ToUpper(strings.TrimSpace(incoming))
	}
	return ids.ULID()
}

// WithCorrelation stores correlation id in context.
func WithCorrelation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, corrKey, id)
}

// CorrelationFromContext returns correlation id if present.
func CorrelationFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(corrKey).(string)
	return v, ok && v != ""
}

// Logger enriches base logger with correlation_id and optional trace/span ids.
func Logger(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	attrs := make([]any, 0, 6)
	if id, ok := CorrelationFromContext(ctx); ok {
		attrs = append(attrs, "correlation_id", id)
	}
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		attrs = append(attrs,
			"trace_id", sc.TraceID().String(),
			"span_id", sc.SpanID().String(),
		)
	}
	if len(attrs) == 0 {
		return base
	}
	return base.With(attrs...)
}

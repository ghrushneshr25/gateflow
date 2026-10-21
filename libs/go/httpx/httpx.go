package httpx

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const HeaderCorrelationID = "X-Correlation-ID"

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code          string         `json:"code"`
	Message       string         `json:"message"`
	CorrelationID string         `json:"correlation_id"`
	Retryable     bool           `json:"retryable"`
	Status        string         `json:"status"`
	Details       map[string]any `json:"details,omitempty"`
}

// CorrelationID returns the request correlation id (after middleware) or normalizes header/generates.
func CorrelationID(r *http.Request) string {
	if id, ok := CorrelationFromContext(r.Context()); ok {
		return id
	}
	return NormalizeCorrelationID(r.Header.Get(HeaderCorrelationID))
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, httpStatus int, code, message, correlationID, status string, retryable bool) {
	WriteJSON(w, httpStatus, ErrorBody{Error: ErrorDetail{
		Code:          code,
		Message:       message,
		CorrelationID: correlationID,
		Retryable:     retryable,
		Status:        status,
	}})
}

// CorrelationMiddleware validates/generates X-Correlation-ID, stores it in context,
// and echoes it on the response. Invalid incoming values are replaced (not trusted).
func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := NormalizeCorrelationID(r.Header.Get(HeaderCorrelationID))
		w.Header().Set(HeaderCorrelationID, id)
		ctx := WithCorrelation(r.Context(), id)
		r = r.WithContext(ctx)
		r.Header.Set(HeaderCorrelationID, id)
		next.ServeHTTP(w, r)
	})
}

// WrapHandler applies metrics RED, OTel HTTP server instrumentation, and correlation.
// Order: otelhttp → metrics → correlation → app
func WrapHandler(service string, h http.Handler) http.Handler {
	inner := CorrelationMiddleware(h)
	inner = metrics.Middleware(service, inner)
	return otelhttp.NewHandler(inner, service,
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// InstrumentedTransport returns an HTTP RoundTripper that propagates W3C trace context
// and X-Correlation-ID from the request context.
func InstrumentedTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	otelRT := otelhttp.NewTransport(base)
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if id, ok := CorrelationFromContext(r.Context()); ok && r.Header.Get(HeaderCorrelationID) == "" {
			r = r.Clone(r.Context())
			r.Header.Set(HeaderCorrelationID, id)
		}
		return otelRT.RoundTrip(r)
	})
}

// NewClient returns an http.Client with trace + correlation propagation.
func NewClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: InstrumentedTransport(http.DefaultTransport),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type Health struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Time    string `json:"time"`
}

func MountHealth(mux *http.ServeMux, service string, ready func() error) {
	metrics.Mount(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, Health{
			Service: service,
			Status:  "ok",
			Time:    time.Now().UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready != nil {
			if err := ready(); err != nil {
				WriteError(w, http.StatusServiceUnavailable, "NOT_READY", err.Error(), CorrelationID(r), "failed", true)
				return
			}
		}
		WriteJSON(w, http.StatusOK, Health{
			Service: service,
			Status:  "ready",
			Time:    time.Now().UTC().Format(time.RFC3339),
		})
	})
}

func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// Package metrics provides low-cardinality Prometheus RED + ops gauges for Gateflow.
//
// Labels are bounded: service, method, route template, status_class (2xx|3xx|4xx|5xx).
// Never label by buyer_id, order_id, event_id, correlation_id, or free-form errors.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateflow_http_requests_total",
		Help: "HTTP requests by service, method, route template, and status class",
	}, []string{"service", "method", "route", "status_class"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gateflow_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"service", "method", "route", "status_class"})

	// Admission
	admissionGrants = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateflow_admission_grants_total",
		Help: "Admission grant attempts by outcome (ok|fenced|budget|not_found|error)",
	}, []string{"outcome"})

	admissionFenced = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_admission_coordination_fenced",
		Help: "1 if admission coordination mode is FENCED, else 0",
	})

	admissionEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_admission_coordination_epoch",
		Help: "Current admission coordination epoch",
	})

	// Order / recon
	orderReconBacklog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_order_reconciliation_required",
		Help: "Count of orders in RECONCILIATION_REQUIRED",
	})

	orderPaymentPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_order_payment_pending",
		Help: "Count of orders in PAYMENT_PENDING",
	})

	orderReconcile = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateflow_order_reconcile_total",
		Help: "Reconcile/advance outcomes (confirmed|still_ambiguous|failed|error)",
	}, []string{"result"})

	// Outbox
	outboxUnpublished = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_outbox_unpublished",
		Help: "Unpublished outbox rows",
	})

	outboxOldestAge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_outbox_oldest_age_seconds",
		Help: "Age in seconds of oldest unpublished outbox row (0 if none)",
	})

	outboxQuarantine = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateflow_outbox_quarantined",
		Help: "Quarantined unpublished outbox rows",
	})

	outboxPublish = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateflow_outbox_publish_total",
		Help: "Outbox publish attempts (ok|fail)",
	}, []string{"result"})

	// Payment simulator
	paymentOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateflow_payment_outcomes_total",
		Help: "Payment create/lookup outcomes by status class label (succeeded|failed|pending|unknown|error)",
	}, []string{"outcome"})

	compensationOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateflow_compensation_total",
		Help: "Compensation attempts by outcome (succeeded|failed|created|error)",
	}, []string{"outcome"})
)

// Handler returns the Prometheus scrape handler for GET /metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Mount registers GET /metrics on mux (unauthenticated local scrape; network isolation assumed).
func Mount(mux *http.ServeMux) {
	mux.Handle("GET /metrics", Handler())
}

// Middleware records RED metrics. route is a low-cardinality template (e.g. /v1/orders/{id}).
func Middleware(service string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		route := RouteTemplate(r.Method, r.URL.Path)
		sc := statusClass(rw.status)
		httpRequests.WithLabelValues(service, r.Method, route, sc).Inc()
		httpDuration.WithLabelValues(service, r.Method, route, sc).Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

// RouteTemplate maps paths to bounded route labels (no IDs).
func RouteTemplate(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if looksLikeID(p) {
			out = append(out, "{id}")
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return "/"
	}
	return "/" + strings.Join(out, "/")
}

func looksLikeID(s string) bool {
	if len(s) >= 20 && strings.Contains(s, "_") {
		return true // ulid-style business ids: ord_01...
	}
	if len(s) == 26 {
		// bare ULID
		ok := true
		for _, c := range s {
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	// numeric only
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	return false
}

// --- setters used by services ---

func AdmissionGrant(outcome string) { admissionGrants.WithLabelValues(outcome).Inc() }

func SetAdmissionFenced(fenced bool, epoch float64) {
	if fenced {
		admissionFenced.Set(1)
	} else {
		admissionFenced.Set(0)
	}
	admissionEpoch.Set(epoch)
}

func SetOrderBacklog(recon, pending float64) {
	orderReconBacklog.Set(recon)
	orderPaymentPending.Set(pending)
}

func OrderReconcile(result string) { orderReconcile.WithLabelValues(result).Inc() }

func SetOutbox(unpublished, quarantine, oldestAgeSec float64) {
	outboxUnpublished.Set(unpublished)
	outboxQuarantine.Set(quarantine)
	outboxOldestAge.Set(oldestAgeSec)
}

func OutboxPublish(result string) { outboxPublish.WithLabelValues(result).Inc() }

func PaymentOutcome(outcome string) { paymentOutcomes.WithLabelValues(outcome).Inc() }

func CompensationOutcome(outcome string) { compensationOutcomes.WithLabelValues(outcome).Inc() }

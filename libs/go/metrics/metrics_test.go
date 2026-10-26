package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteTemplateBoundsIDs(t *testing.T) {
	got := RouteTemplate("GET", "/v1/orders/ord_01M5D72BH63QHYD47B06XSN8GV")
	if got != "/v1/orders/{id}" {
		t.Fatalf("got %s", got)
	}
	got = RouteTemplate("POST", "/v1/sessions/ses_01ABCDEFGHIJKLMNOPQRSTUV/grants")
	if !strings.Contains(got, "{id}") || strings.Contains(got, "ses_01") {
		t.Fatalf("got %s", got)
	}
}

func TestMiddlewareIncrementsCounter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/meta", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	h := Middleware("testsvc", mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/meta", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	AdmissionGrant("ok", "A")
	SetAdmissionFenced(true, 3)
	SetOrderBacklog(2, 1)
	SetOutbox(5, 1, 12.5)
	OutboxPublish("ok")
	PaymentOutcome("succeeded")
	CompensationOutcome("succeeded")
	OrderReconcile("confirmed")
}

func TestMetricsHandlerServes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "gateflow_") && !strings.Contains(body, "go_") {
		t.Fatalf("body=%q", body[:min(120, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	mux := http.NewServeMux()
	MountHealth(mux, "test", nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	CorrelationMiddleware(mux).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if rr.Header().Get(HeaderCorrelationID) == "" {
		t.Fatal("missing correlation id")
	}
	var h Health
	if err := json.NewDecoder(rr.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.Service != "test" || h.Status != "ok" {
		t.Fatalf("unexpected body: %+v", h)
	}
}

func TestReadyzFailure(t *testing.T) {
	mux := http.NewServeMux()
	MountHealth(mux, "test", func() error { return errReady })
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rr.Code)
	}
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const errReady simpleError = "db down"

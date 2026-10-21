package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
)

func TestSaleMutatorRequiresOperator(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	mux.HandleFunc("POST /v1/offerings", func(w http.ResponseWriter, r *http.Request) {
		if !s.requireOps(w, r, "c", authn.ScopeSaleWrite) {
			return
		}
		w.WriteHeader(204)
	})
	h := authn.Middleware(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/offerings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("missing got %d", rec.Code)
	}

	svc, _ := authn.MintServiceJWT(authn.DefaultDevSecret, "s", []string{authn.ScopeInventoryWrite}, time.Hour)
	req = httptest.NewRequest(http.MethodPost, "/v1/offerings", nil)
	req.Header.Set("Authorization", "Bearer "+svc)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("service got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/offerings", nil)
	req.Header.Set("X-Ops-Role", "operator")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("spoof got %d", rec.Code)
	}

	op, _ := authn.MintOperatorJWT(authn.DefaultDevSecret, "op", []string{authn.ScopeSaleWrite}, time.Hour)
	req = httptest.NewRequest(http.MethodPost, "/v1/offerings", nil)
	req.Header.Set("Authorization", "Bearer "+op)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("ops got %d body=%s", rec.Code, rec.Body.String())
	}
}

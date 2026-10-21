package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
)

func TestInventoryMutatorRequiresService(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	mux.HandleFunc("POST /v1/pools", func(w http.ResponseWriter, r *http.Request) {
		if !s.requireService(w, r, "c") {
			return
		}
		w.WriteHeader(204)
	})
	h := authn.Middleware(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/pools", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("missing %d", rec.Code)
	}

	op, _ := authn.MintOperatorJWT(authn.DefaultDevSecret, "op", []string{authn.ScopeSaleWrite}, time.Hour)
	req = httptest.NewRequest(http.MethodPost, "/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer "+op)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("operator %d", rec.Code)
	}

	svc, _ := authn.MintServiceJWT(authn.DefaultDevSecret, "s", []string{authn.ScopeInventoryWrite}, time.Hour)
	req = httptest.NewRequest(http.MethodPost, "/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer "+svc)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("service %d", rec.Code)
	}
}

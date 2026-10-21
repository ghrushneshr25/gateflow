package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func TestAdminReconciliationRejectsSpoofedHeaders(t *testing.T) {
	ms := &mockStore{byKey: map[string]store.Order{}}
	s := httpserver.NewWithDeps(ms, mockHTTP{}, httpserver.Deps{})
	mux := http.NewServeMux()
	s.Routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/reconciliation", nil)
	req.Header.Set("X-Ops-Role", "admin")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestAdminReconciliationAcceptsOperatorJWT(t *testing.T) {
	ms := &mockStore{byKey: map[string]store.Order{}}
	s := httpserver.NewWithDeps(ms, mockHTTP{}, httpserver.Deps{})
	mux := http.NewServeMux()
	s.Routes(mux)
	handler := authn.Middleware(mux)
	tok, err := authn.MintOperatorJWT(authn.DefaultDevSecret, "ops1", []string{authn.ScopeOrderRecover}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/reconciliation", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetOrderBuyerJWTOwnership(t *testing.T) {
	ms := &mockStore{
		byID: map[string]store.Order{
			"ord_1": {ID: "ord_1", BuyerID: "buyer_a", Status: "CONFIRMED"},
		},
	}
	s := httpserver.NewWithDeps(ms, mockHTTP{}, httpserver.Deps{})
	mux := http.NewServeMux()
	s.Routes(mux)
	h := authn.Middleware(mux)

	// header only — rejected
	req := httptest.NewRequest(http.MethodGet, "/v1/orders/ord_1", nil)
	req.Header.Set("X-Buyer-ID", "buyer_a")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("header only %d", rr.Code)
	}

	// wrong buyer JWT
	tokB, _ := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_b", nil, time.Hour)
	req = httptest.NewRequest(http.MethodGet, "/v1/orders/ord_1", nil)
	req.Header.Set("Authorization", "Bearer "+tokB)
	req.Header.Set("X-Buyer-ID", "buyer_a") // spoof ignored
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("cross buyer %d %s", rr.Code, rr.Body.String())
	}

	// owner JWT
	tokA, _ := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_a", nil, time.Hour)
	req = httptest.NewRequest(http.MethodGet, "/v1/orders/ord_1", nil)
	req.Header.Set("Authorization", "Bearer "+tokA)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("owner %d %s", rr.Code, rr.Body.String())
	}
}

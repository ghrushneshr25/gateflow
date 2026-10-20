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
	// Wrap without authn middleware — claims absent; spoofed headers must fail
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/reconciliation", nil)
	req.Header.Set("X-Ops-Role", "admin")
	req.Header.Set("X-Service-Name", "evil")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAdminReconciliationAcceptsOperatorJWT(t *testing.T) {
	ms := &mockStore{byKey: map[string]store.Order{}}
	s := httpserver.NewWithDeps(ms, mockHTTP{}, httpserver.Deps{})
	mux := http.NewServeMux()
	s.Routes(mux)
	handler := authn.Middleware(mux)

	signer := authn.NewDevSigner("gateflow-dev-secret-change-me")
	tok, err := signer.Sign(authn.Claims{
		Subject: "ops1", PrincipalType: "operator",
		Scopes: []string{"ops:order:recover"},
	}, time.Hour)
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

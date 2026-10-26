package httpserver_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

func TestGrantProtocolBUsesProtocolBPath(t *testing.T) {
	spy := &grantSpyStore{}
	s := httpserver.NewWithProtocol(spy, "B")
	mux := http.NewServeMux()
	s.Routes(mux)
	h := authn.Middleware(mux)
	tok, err := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_proto_b", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if spy.callCount != 1 || spy.lastPath != "B" {
		t.Fatalf("calls=%d path=%s", spy.callCount, spy.lastPath)
	}
}

func TestGrantProtocolInvalidDefaultsA(t *testing.T) {
	s := httpserver.NewWithProtocol(&grantSpyStore{}, "Z")
	if s.GrantProtocol != "A" {
		t.Fatalf("%s", s.GrantProtocol)
	}
}

func TestGrantStaleGenerationMapsTo503(t *testing.T) {
	spy := &grantSpyStore{err: store.ErrStaleGeneration}
	s := httpserver.NewWithProtocol(spy, "B")
	mux := http.NewServeMux()
	s.Routes(mux)
	h := authn.Middleware(mux)
	tok, _ := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_stale", nil, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 503 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

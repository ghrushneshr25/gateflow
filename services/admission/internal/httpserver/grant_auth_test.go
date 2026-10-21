package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

// grantSpyStore records GrantFIFO calls and returns configurable results.
type grantSpyStore struct {
	mu       sync.Mutex
	calls    []grantCall
	entry    store.QueueEntry
	ent      store.Entitlement
	err      error
	callCount int
}

type grantCall struct {
	SessionID, BuyerID, EntID string
}

func (g *grantSpyStore) Join(context.Context, store.QueueEntry) (store.QueueEntry, error) {
	return store.QueueEntry{}, nil
}
func (g *grantSpyStore) GetMine(context.Context, string, string) (store.QueueEntry, error) {
	return store.QueueEntry{}, store.ErrNotFound
}
func (g *grantSpyStore) GrantFIFO(_ context.Context, sessionID, buyerID, entID string, _ time.Duration, _ time.Time) (store.QueueEntry, store.Entitlement, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.callCount++
	g.calls = append(g.calls, grantCall{sessionID, buyerID, entID})
	if g.err != nil {
		return store.QueueEntry{}, store.Entitlement{}, g.err
	}
	e := g.entry
	if e.ID == "" {
		e = store.QueueEntry{ID: "q1", SessionID: sessionID, BuyerID: buyerID, Status: "GRANTED"}
		eid := entID
		e.EntitlementID = &eid
	}
	ent := g.ent
	if ent.ID == "" {
		ent = store.Entitlement{ID: entID, SessionID: sessionID, BuyerID: buyerID, Status: "ISSUED"}
	}
	return e, ent, nil
}
func (g *grantSpyStore) GetEntitlement(context.Context, string) (store.Entitlement, error) {
	return store.Entitlement{}, store.ErrNotFound
}
func (g *grantSpyStore) Bind(context.Context, string, string, string, time.Time) (store.Entitlement, error) {
	return store.Entitlement{}, nil
}
func (g *grantSpyStore) Consume(context.Context, string, string, time.Time) (store.Entitlement, error) {
	return store.Entitlement{}, nil
}
func (g *grantSpyStore) EnsureBudgets(context.Context, string, int, int) error { return nil }
func (g *grantSpyStore) GetBudget(context.Context, string) (store.Budget, error) {
	return store.Budget{}, store.ErrNotFound
}
func (g *grantSpyStore) SetBudgetCaps(context.Context, string, int, int) error { return nil }
func (g *grantSpyStore) GetCoordination(context.Context) (store.Coordination, error) {
	return store.Coordination{ID: "admission", Mode: "ACTIVE", Epoch: 1}, nil
}
func (g *grantSpyStore) FenceAdmission(context.Context, string, string, time.Time) (store.Coordination, error) {
	return store.Coordination{}, nil
}
func (g *grantSpyStore) UnfenceAdmission(context.Context, string, time.Time) (store.Coordination, error) {
	return store.Coordination{}, nil
}

func grantHandler(store httpserver.AdmissionStore) http.Handler {
	s := httpserver.New(store)
	mux := http.NewServeMux()
	s.Routes(mux)
	return authn.Middleware(mux)
}

func TestGrantBuyerSelfOnlyIgnoresBodyTarget(t *testing.T) {
	spy := &grantSpyStore{}
	h := grantHandler(spy)
	tok, err := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_a", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"buyer_id": "buyer_OTHER"})
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(spy.calls) != 1 || spy.calls[0].BuyerID != "buyer_a" {
		t.Fatalf("calls=%+v want buyer_a only", spy.calls)
	}
}

func TestGrantBuyerCannotUseServicePathWithoutQueueIsStore(t *testing.T) {
	// not-in-queue → store ErrNotFound → 404, no silent success
	spy := &grantSpyStore{err: store.ErrNotFound}
	h := grantHandler(spy)
	tok, _ := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_a", nil, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 404 {
		t.Fatalf("status=%d", rr.Code)
	}
	if spy.callCount != 1 {
		t.Fatal("expected GrantFIFO attempted")
	}
}

func TestGrantFencedReturns503(t *testing.T) {
	spy := &grantSpyStore{err: store.ErrCoordinationFenced}
	h := grantHandler(spy)
	tok, _ := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_a", nil, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 503 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGrantBudgetExhausted429(t *testing.T) {
	spy := &grantSpyStore{err: store.ErrBudgetExhausted}
	h := grantHandler(spy)
	tok, _ := authn.MintBuyerJWT(authn.DefaultDevSecret, "buyer_a", nil, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 429 {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestGrantServiceRequiresScopeAndBody(t *testing.T) {
	spy := &grantSpyStore{}
	h := grantHandler(spy)
	// service without grant scope
	tok, _ := authn.MintServiceJWT(authn.DefaultDevSecret, "svc", []string{authn.ScopeInventoryWrite}, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{"buyer_id":"b1"}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == 200 {
		t.Fatal("wrong scope must not grant")
	}
	if spy.callCount != 0 {
		t.Fatalf("GrantFIFO must not run, calls=%d", spy.callCount)
	}

	// service with grant scope
	tok, _ = authn.MintServiceJWT(authn.DefaultDevSecret, "svc", []string{authn.ScopeAdmissionGrant}, time.Hour)
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{"buyer_id":"buyer_svc_target"}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d %s", rr.Code, rr.Body.String())
	}
	if spy.calls[0].BuyerID != "buyer_svc_target" {
		t.Fatalf("%+v", spy.calls)
	}
}

func TestGrantRejectsMissingTokenAndSpoofHeaders(t *testing.T) {
	spy := &grantSpyStore{}
	h := grantHandler(spy)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{"buyer_id":"x"}`)))
	req.Header.Set("X-Buyer-ID", "x")
	req.Header.Set("X-Ops-Role", "service")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("status=%d", rr.Code)
	}
	if spy.callCount != 0 {
		t.Fatal("must not call store")
	}
}

func TestGrantOperatorRejected(t *testing.T) {
	spy := &grantSpyStore{}
	h := grantHandler(spy)
	tok, _ := authn.MintOperatorJWT(authn.DefaultDevSecret, "ops", []string{authn.ScopeAdmissionFence}, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions/ses_1/grants", bytes.NewReader([]byte(`{"buyer_id":"b"}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == 200 || spy.callCount != 0 {
		t.Fatalf("operator must not grant code=%d calls=%d", rr.Code, spy.callCount)
	}
}

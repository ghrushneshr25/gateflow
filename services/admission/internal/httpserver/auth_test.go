package httpserver_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

type mockCoordStore struct {
	coord store.Coordination
}

func (m *mockCoordStore) Join(context.Context, store.QueueEntry) (store.QueueEntry, error) {
	return store.QueueEntry{}, nil
}
func (m *mockCoordStore) GetMine(context.Context, string, string) (store.QueueEntry, error) {
	return store.QueueEntry{}, store.ErrNotFound
}
func (m *mockCoordStore) GrantFIFO(context.Context, string, string, string, time.Duration, time.Time) (store.QueueEntry, store.Entitlement, error) {
	return store.QueueEntry{}, store.Entitlement{}, store.ErrCoordinationFenced
}
func (m *mockCoordStore) GetEntitlement(context.Context, string) (store.Entitlement, error) {
	return store.Entitlement{}, store.ErrNotFound
}
func (m *mockCoordStore) Bind(context.Context, string, string, string, time.Time) (store.Entitlement, error) {
	return store.Entitlement{}, nil
}
func (m *mockCoordStore) Consume(context.Context, string, string, time.Time) (store.Entitlement, error) {
	return store.Entitlement{}, nil
}
func (m *mockCoordStore) EnsureBudgets(context.Context, string, int, int) error { return nil }
func (m *mockCoordStore) GetBudget(context.Context, string) (store.Budget, error) {
	return store.Budget{}, store.ErrNotFound
}
func (m *mockCoordStore) SetBudgetCaps(context.Context, string, int, int) error { return nil }
func (m *mockCoordStore) GetCoordination(context.Context) (store.Coordination, error) {
	return m.coord, nil
}
func (m *mockCoordStore) FenceAdmission(_ context.Context, reason, actor string, now time.Time) (store.Coordination, error) {
	m.coord.Mode = store.CoordModeFenced
	m.coord.Epoch++
	r := reason
	m.coord.FenceReason = &r
	m.coord.FencedAt = &now
	m.coord.FencedBy = &actor
	return m.coord, nil
}
func (m *mockCoordStore) UnfenceAdmission(_ context.Context, actor string, now time.Time) (store.Coordination, error) {
	m.coord.Mode = store.CoordModeActive
	m.coord.FenceReason = nil
	m.coord.FencedAt = nil
	m.coord.FencedBy = &actor
	m.coord.UpdatedAt = now
	return m.coord, nil
}

func TestCoordinationAPIRejectsSpoofedHeaders(t *testing.T) {
	s := httpserver.New(&mockCoordStore{coord: store.Coordination{ID: "admission", Mode: "ACTIVE", Epoch: 1}})
	mux := http.NewServeMux()
	s.Routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/coordination", nil)
	req.Header.Set("X-Ops-Role", "admin")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestCoordinationAPIAcceptsJWT(t *testing.T) {
	s := httpserver.New(&mockCoordStore{coord: store.Coordination{ID: "admission", Mode: "ACTIVE", Epoch: 1}})
	mux := http.NewServeMux()
	s.Routes(mux)
	h := authn.Middleware(mux)
	tok, err := authn.NewDevSigner("gateflow-dev-secret-change-me").Sign(authn.Claims{
		Subject: "ops", PrincipalType: "operator", Scopes: []string{"ops:admission:read", "ops:admission:fence"},
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/coordination", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

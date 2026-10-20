package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ghrushneshr25/nexus"
	"github.com/ghrushneshrathod/gateflow/libs/go/appnexus"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/app"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/ports"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// mockStore is a test double registered through nexus.
type mockStore struct {
	byKey map[string]store.Order
}

func (m *mockStore) GetByIdempotency(_ context.Context, buyerID, key string) (store.Order, error) {
	o, ok := m.byKey[buyerID+"|"+key]
	if !ok {
		return store.Order{}, store.ErrNotFound
	}
	return o, nil
}
func (m *mockStore) Get(context.Context, string) (store.Order, error) {
	return store.Order{}, store.ErrNotFound
}
func (m *mockStore) ListByBuyer(context.Context, string) ([]store.Order, error) { return nil, nil }
func (m *mockStore) ListByStatus(context.Context, string, int) ([]store.Order, error) {
	return nil, nil
}
func (m *mockStore) CreateWithHold(context.Context, store.Order, string, time.Time) (store.Order, error) {
	return store.Order{}, store.ErrLimit
}
func (m *mockStore) Update(context.Context, store.Order) error { return nil }
func (m *mockStore) Confirm(context.Context, string, string, string, any, time.Time) (store.Order, error) {
	return store.Order{}, nil
}
func (m *mockStore) Fail(context.Context, string, time.Time) error { return nil }
func (m *mockStore) HeldAndCommittedQty(context.Context, string, string) (int, error) {
	return 0, nil
}
func (m *mockStore) MarkEventProcessed(context.Context, string) (bool, error) { return true, nil }
func (m *mockStore) CountOutboxForAggregate(context.Context, string) (int, error) { return 0, nil }
func (m *mockStore) CountOutboxByEventID(context.Context, string) (int, error)  { return 0, nil }

type mockHTTP struct{}

func (mockHTTP) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{"max_per_buyer":4,"version":1}`)),
		Header:     make(http.Header),
	}, nil
}

func TestCheckoutIdempotentReplay_ViaNexusMock(t *testing.T) {
	appnexus.ResetForTest()

	existing := store.Order{
		ID: "ord_existing", BuyerID: "buyer_1", SessionID: "ses_1",
		TicketTypeID: "tt", PoolID: "pool", Quantity: 1, Status: "CONFIRMED",
		EntitlementID: "ent", IdempotencyKey: "key-1", RequestHash: "",
	}
	// Precompute hash empty path uses field compare; set matching fields.

	ms := &mockStore{byKey: map[string]store.Order{
		"buyer_1|key-1": existing,
	}}

	nexus.MustDeclare(func() ports.OrderStore { return ms })
	nexus.MustDeclare(func() ports.HTTPDoer { return mockHTTP{} })
	nexus.MustDeclare(func(st ports.OrderStore, h ports.HTTPDoer) ports.APIServer {
		return httpserver.NewWithDeps(st, h, httpserver.Deps{
			SaleBase: "http://sale", AdmissionBase: "http://adm",
			InventoryBase: "http://inv", PaymentBase: "http://pay",
		})
	})
	if err := nexus.Validate(); err != nil {
		t.Fatal(err)
	}

	api := nexus.MustGet(app.APIServerContract)
	mux := http.NewServeMux()
	api.Routes(mux)

	body, _ := json.Marshal(map[string]any{
		"buyer_id": "buyer_1", "session_id": "ses_1", "ticket_type_id": "tt",
		"pool_id": "pool", "entitlement_id": "ent", "quantity": 1,
		"amount_cents": 100, "idempotency_key": "key-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/checkouts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Buyer-ID", "buyer_1")
	req.Header.Set("Idempotency-Key", "key-1")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got store.Order
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "ord_existing" || got.Status != "CONFIRMED" {
		t.Fatalf("unexpected: %+v", got)
	}
}

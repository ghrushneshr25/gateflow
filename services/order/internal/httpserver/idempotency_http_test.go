package httpserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ghrushneshr25/nexus"
	"github.com/ghrushneshrathod/gateflow/libs/go/appnexus"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/app"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/ports"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func TestHTTPIdempotencyConflict(t *testing.T) {
	appnexus.ResetForTest()
	existing := store.Order{
		ID: "ord_x", BuyerID: "buyer_1", SessionID: "ses_1",
		TicketTypeID: "tt", PoolID: "pool", Quantity: 1, Status: "CONFIRMED",
		EntitlementID: "ent", IdempotencyKey: "key-1", RequestHash: "hash-a",
	}
	ms := &mockStore{byKey: map[string]store.Order{"buyer_1|key-1": existing}}
	nexus.MustDeclare(func() ports.OrderStore { return ms })
	nexus.MustDeclare(func() ports.HTTPDoer { return mockHTTP{} })
	nexus.MustDeclare(func(st ports.OrderStore, h ports.HTTPDoer) ports.APIServer {
		return httpserver.NewWithDeps(st, h, httpserver.Deps{})
	})
	api := nexus.MustGet(app.APIServerContract)
	mux := http.NewServeMux()
	api.Routes(mux)

	body, _ := json.Marshal(map[string]any{
		"buyer_id": "buyer_1", "session_id": "ses_OTHER", "ticket_type_id": "tt",
		"pool_id": "pool", "entitlement_id": "ent", "quantity": 2,
		"amount_cents": 100, "idempotency_key": "key-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/checkouts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Buyer-ID", "buyer_1")
	req.Header.Set("Idempotency-Key", "key-1")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != 409 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var env map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	errObj, _ := env["error"].(map[string]any)
	if errObj["code"] != "ORD_IDEMPOTENCY_CONFLICT" {
		t.Fatalf("%v", env)
	}
}

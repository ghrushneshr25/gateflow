package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// Deps holds outbound base URLs for S2S orchestration.
type Deps struct {
	SaleBase      string
	AdmissionBase string
	InventoryBase string
	PaymentBase   string
}

// OrderStore is satisfied by *store.Store and test doubles.
type OrderStore interface {
	GetByIdempotency(ctx context.Context, buyerID, key string) (store.Order, error)
	Get(ctx context.Context, id string) (store.Order, error)
	ListByBuyer(ctx context.Context, buyerID string) ([]store.Order, error)
	CreateWithHold(ctx context.Context, o store.Order, holdID string, now time.Time) (store.Order, error)
	Update(ctx context.Context, o store.Order) error
	Confirm(ctx context.Context, orderID, eventID, corr string, payload any, now time.Time) (store.Order, error)
	Fail(ctx context.Context, orderID string, now time.Time) error
}

// HTTPDoer is satisfied by *http.Client and test doubles.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Server struct {
	Store         OrderStore
	SaleBase      string
	AdmissionBase string
	InventoryBase string
	PaymentBase   string
	HTTP          HTTPDoer
}

// New is a convenience constructor for production defaults.
func New(st *store.Store) *Server {
	return NewWithDeps(st, &http.Client{Timeout: 5 * time.Second}, Deps{
		SaleBase:      env("SALE_BASE_URL", "http://127.0.0.1:8081"),
		AdmissionBase: env("ADMISSION_BASE_URL", "http://127.0.0.1:8082"),
		InventoryBase: env("INVENTORY_BASE_URL", "http://127.0.0.1:8083"),
		PaymentBase:   env("PAYMENT_BASE_URL", "http://127.0.0.1:8085"),
	})
}

// NewWithDeps constructs the API server with injected ports (nexus / tests).
func NewWithDeps(st OrderStore, httpDoer HTTPDoer, deps Deps) *Server {
	if httpDoer == nil {
		httpDoer = &http.Client{Timeout: 5 * time.Second}
	}
	return &Server{
		Store:         st,
		SaleBase:      deps.SaleBase,
		AdmissionBase: deps.AdmissionBase,
		InventoryBase: deps.InventoryBase,
		PaymentBase:   deps.PaymentBase,
		HTTP:          httpDoer,
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/checkouts", s.checkout)
	mux.HandleFunc("GET /v1/orders/{id}", s.getOrder)
	mux.HandleFunc("GET /v1/orders", s.listOrders)
}

func buyer(r *http.Request) string {
	if b := r.Header.Get("X-Buyer-ID"); b != "" {
		return b
	}
	return r.URL.Query().Get("buyer_id")
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	b := buyer(r)
	if b == "" {
		httpx.WriteError(w, 401, "ORD_UNAUTHORIZED", "buyer required", corr, "failed", false)
		return
	}
	items, err := s.Store.ListByBuyer(r.Context(), b)
	if err != nil {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if items == nil {
		items = []store.Order{}
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	o, err := s.Store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ORD_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, o)
}

func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	b := buyer(r)
	var req struct {
		BuyerID        string `json:"buyer_id"`
		SessionID      string `json:"session_id"`
		TicketTypeID   string `json:"ticket_type_id"`
		PoolID         string `json:"pool_id"`
		EntitlementID  string `json:"entitlement_id"`
		Quantity       int    `json:"quantity"`
		AmountCents    int64  `json:"amount_cents"`
		Currency       string `json:"currency"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "ORD_VALIDATION_FAILED", "invalid body", corr, "failed", false)
		return
	}
	if b == "" {
		b = req.BuyerID
	}
	if b == "" || req.SessionID == "" || req.TicketTypeID == "" || req.PoolID == "" || req.EntitlementID == "" || req.Quantity <= 0 {
		httpx.WriteError(w, 400, "ORD_VALIDATION_FAILED", "missing required fields", corr, "failed", false)
		return
	}
	key := req.IdempotencyKey
	if key == "" {
		key = r.Header.Get("Idempotency-Key")
	}
	if key == "" {
		httpx.WriteError(w, 400, "ORD_VALIDATION_FAILED", "Idempotency-Key required", corr, "failed", false)
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	reqHash := checkoutRequestHash(req.SessionID, req.TicketTypeID, req.PoolID, req.EntitlementID, req.Quantity, req.AmountCents)

	if existing, err := s.Store.GetByIdempotency(r.Context(), b, key); err == nil {
		if existing.RequestHash != "" && existing.RequestHash != reqHash {
			httpx.WriteError(w, 409, "ORD_IDEMPOTENCY_CONFLICT", "conflicting idempotency key reuse", corr, "conflict", false)
			return
		}
		if existing.SessionID != req.SessionID || existing.TicketTypeID != req.TicketTypeID || existing.Quantity != req.Quantity || existing.PoolID != req.PoolID {
			httpx.WriteError(w, 409, "ORD_IDEMPOTENCY_CONFLICT", "conflicting idempotency key reuse", corr, "conflict", false)
			return
		}
		// Lost-response safe: return established order for any status (including in-progress).
		// Do not start a second logical checkout.
		httpx.WriteJSON(w, 200, existing)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}

	// Policy snapshot
	policy, err := s.getJSON(r, s.SaleBase+"/v1/sessions/"+req.SessionID+"/purchase-policy", corr)
	if err != nil {
		httpx.WriteError(w, 502, "ORD_SALE_UNAVAILABLE", err.Error(), corr, "failed", true)
		return
	}
	maxPerBuyer := intFrom(policy["max_per_buyer"], 4)
	policyVersion := intFrom(policy["version"], 1)

	now := time.Now().UTC()
	checkoutOp := ids.New("chk")
	intendedID := ids.New("ord")
	o := store.Order{
		ID: intendedID, BuyerID: b, SessionID: req.SessionID, TicketTypeID: req.TicketTypeID,
		PoolID: req.PoolID, Quantity: req.Quantity, Status: "CREATED", EntitlementID: req.EntitlementID,
		CheckoutOperationID: checkoutOp, InventoryOperationID: ids.New("invop"), PaymentOperationID: ids.New("payop"),
		AmountCents: req.AmountCents, Currency: req.Currency, PolicyVersion: policyVersion, MaxPerBuyer: maxPerBuyer,
		IdempotencyKey: key, RequestHash: reqHash,
	}
	o, err = s.Store.CreateWithHold(r.Context(), o, ids.New("hold"), now)
	if errors.Is(err, store.ErrLimit) {
		httpx.WriteError(w, 409, "ORD_PURCHASE_LIMIT_EXCEEDED", "purchase limit exceeded", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrIdempotency) {
		httpx.WriteError(w, 409, "ORD_IDEMPOTENCY_CONFLICT", "conflicting idempotency key reuse", corr, "conflict", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	// Concurrent first-create lost the insert race: return established order; do not dual-orchestrate.
	if o.ID != intendedID {
		httpx.WriteJSON(w, 200, o)
		return
	}
	if o.Status != "CREATED" {
		httpx.WriteJSON(w, 200, o)
		return
	}

	// Bind entitlement
	bindBody, _ := json.Marshal(map[string]string{
		"buyer_id": b, "checkout_operation_id": o.CheckoutOperationID,
	})
	if _, err := s.postJSON(r, s.AdmissionBase+"/v1/entitlements/"+req.EntitlementID+"/bind", bindBody, corr); err != nil {
		_ = s.Store.Fail(r.Context(), o.ID, time.Now().UTC())
		httpx.WriteError(w, 409, "ORD_ENTITLEMENT_INVALID", err.Error(), corr, "failed", false)
		return
	}
	o.Status = "ENTITLEMENT_BOUND"
	o.UpdatedAt = time.Now().UTC()
	_ = s.Store.Update(r.Context(), o)

	// Reserve inventory
	resBody, _ := json.Marshal(map[string]any{
		"operation_id": o.InventoryOperationID, "pool_id": o.PoolID, "quantity": o.Quantity,
		"buyer_id": o.BuyerID, "order_id": o.ID, "lease_seconds": 300,
	})
	res, err := s.postJSON(r, s.InventoryBase+"/v1/reservations", resBody, corr)
	if err != nil {
		_ = s.Store.Fail(r.Context(), o.ID, time.Now().UTC())
		httpx.WriteError(w, 409, "ORD_RESERVATION_FAILED", err.Error(), corr, "failed", false)
		return
	}
	resID, _ := res["id"].(string)
	o.ReservationID = &resID
	o.Status = "RESERVATION_HELD"
	o.UpdatedAt = time.Now().UTC()
	_ = s.Store.Update(r.Context(), o)

	// Consume entitlement
	consBody, _ := json.Marshal(map[string]string{"checkout_operation_id": o.CheckoutOperationID})
	_, _ = s.postJSON(r, s.AdmissionBase+"/v1/entitlements/"+req.EntitlementID+"/consume", consBody, corr)

	// Payment
	payBody, _ := json.Marshal(map[string]any{
		"operation_id": o.PaymentOperationID, "order_id": o.ID,
		"amount_cents": o.AmountCents, "currency": o.Currency, "scenario": "success",
	})
	pay, err := s.postJSON(r, s.PaymentBase+"/v1/payments", payBody, corr)
	if err != nil {
		httpx.WriteError(w, 502, "ORD_PAYMENT_FAILED", err.Error(), corr, "failed", true)
		return
	}
	payID, _ := pay["id"].(string)
	o.PaymentID = &payID
	status, _ := pay["status"].(string)
	if status != "SUCCEEDED" {
		_ = s.Store.Fail(r.Context(), o.ID, time.Now().UTC())
		o.Status = "FAILED"
		httpx.WriteJSON(w, 200, o)
		return
	}
	o.Status = "PAYMENT_SUCCEEDED"
	o.UpdatedAt = time.Now().UTC()
	_ = s.Store.Update(r.Context(), o)

	// Confirm inventory
	conf, err := s.postJSON(r, s.InventoryBase+"/v1/reservations/"+resID+"/confirm", []byte("{}"), corr)
	if err != nil {
		o.Status = "RECONCILIATION_REQUIRED"
		o.UpdatedAt = time.Now().UTC()
		_ = s.Store.Update(r.Context(), o)
		httpx.WriteJSON(w, 200, o)
		return
	}
	if confStatus, _ := conf["status"].(string); confStatus != "CONFIRMED" {
		o.Status = "RECONCILIATION_REQUIRED"
		o.UpdatedAt = time.Now().UTC()
		_ = s.Store.Update(r.Context(), o)
		httpx.WriteJSON(w, 200, o)
		return
	}

	o, err = s.Store.Confirm(r.Context(), o.ID, ids.New("evt"), corr, map[string]any{
		"order_id": o.ID, "buyer_id": o.BuyerID, "quantity": o.Quantity, "session_id": o.SessionID,
	}, time.Now().UTC())
	if err != nil {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, o)
}

func (s *Server) getJSON(r *http.Request, url, corr string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(httpx.HeaderCorrelationID, corr)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", string(b))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) postJSON(r *http.Request, url string, body []byte, corr string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderCorrelationID, corr)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", string(b))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func intFrom(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	default:
		return def
	}
}

func checkoutRequestHash(sessionID, ticketTypeID, poolID, entitlementID string, qty int, amount int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d|%d", sessionID, ticketTypeID, poolID, entitlementID, qty, amount)))
	return hex.EncodeToString(sum[:])
}

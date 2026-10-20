package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/orchestrate"
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
	ListByStatus(ctx context.Context, status string, limit int) ([]store.Order, error)
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
	// Driver is optional; if nil, built from HTTP deps.
	Driver *orchestrate.Driver
	// PaymentScenario overrides default success (tests / env).
	PaymentScenario string
}

func New(st *store.Store) *Server {
	return NewWithDeps(st, &http.Client{Timeout: 5 * time.Second}, Deps{
		SaleBase:      env("SALE_BASE_URL", "http://127.0.0.1:8081"),
		AdmissionBase: env("ADMISSION_BASE_URL", "http://127.0.0.1:8082"),
		InventoryBase: env("INVENTORY_BASE_URL", "http://127.0.0.1:8083"),
		PaymentBase:   env("PAYMENT_BASE_URL", "http://127.0.0.1:8085"),
	})
}

func NewWithDeps(st OrderStore, httpDoer HTTPDoer, deps Deps) *Server {
	if httpDoer == nil {
		httpDoer = &http.Client{Timeout: 5 * time.Second}
	}
	s := &Server{
		Store:           st,
		SaleBase:        deps.SaleBase,
		AdmissionBase:   deps.AdmissionBase,
		InventoryBase:   deps.InventoryBase,
		PaymentBase:     deps.PaymentBase,
		HTTP:            httpDoer,
		PaymentScenario: env("PAYMENT_SCENARIO", "success"),
	}
	lease := 300
	if v := os.Getenv("ORDER_LEASE_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			lease = n
		}
	}
	s.Driver = &orchestrate.Driver{
		Store: st,
		Downstream: &orchestrate.HTTPDownstream{
			HTTP: httpDoer, AdmissionBase: deps.AdmissionBase,
			InventoryBase: deps.InventoryBase, PaymentBase: deps.PaymentBase,
			ServiceToken: serviceTokenFromEnv(),
		},
		PaymentScenario: s.PaymentScenario,
		LeaseSeconds:    lease,
	}
	return s
}

// serviceTokenFromEnv returns GATEFLOW_SERVICE_TOKEN or a minted service JWT for S2S recover calls.
func serviceTokenFromEnv() string {
	if t := os.Getenv("GATEFLOW_SERVICE_TOKEN"); t != "" {
		return t
	}
	signer := authn.NewDevSigner(os.Getenv("GATEFLOW_DEV_JWT_SECRET"))
	tok, err := signer.Sign(authn.Claims{
		Subject:       "order-service",
		PrincipalType: "service",
		Scopes:        []string{"ops:payment:recover", "ops:order:recover", "*"},
	}, 24*time.Hour)
	if err != nil {
		return ""
	}
	return tok
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
	mux.HandleFunc("POST /v1/orders/{id}/reconcile", s.reconcile)
	mux.HandleFunc("GET /v1/admin/reconciliation", s.listReconciliation)
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
	b := buyer(r)
	if b != "" && o.BuyerID != b {
		httpx.WriteError(w, 403, "ORD_FORBIDDEN", "not owner", corr, "failed", false)
		return
	}
	httpx.WriteJSON(w, 200, o)
}

func (s *Server) requireOpsOrService(w http.ResponseWriter, r *http.Request, corr, scope string) bool {
	c, ok := authn.ClaimsFromContext(r.Context())
	if !ok || !authn.IsOperatorOrService(c, scope) {
		// Accept operator/service with recover scopes
		if ok && (c.PrincipalType == "operator" || c.PrincipalType == "service") {
			if authn.HasScope(c, scope) || authn.HasScope(c, "ops:order:recover") || authn.HasScope(c, "*") {
				return true
			}
		}
		httpx.WriteError(w, 401, "ORD_UNAUTHORIZED", "valid operator/service bearer token required", corr, "failed", false)
		return false
	}
	return true
}

func (s *Server) listReconciliation(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOpsOrService(w, r, corr, "ops:order:recover") {
		return
	}
	items, err := s.Store.ListByStatus(r.Context(), orchestrate.StatusReconciliationRequired, 100)
	if err != nil {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if items == nil {
		items = []store.Order{}
	}
	pending, _ := s.Store.ListByStatus(r.Context(), orchestrate.StatusPaymentPending, 100)
	httpx.WriteJSON(w, 200, map[string]any{
		"reconciliation_required": items,
		"payment_pending":         pending,
	})
}

func (s *Server) reconcile(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	id := r.PathValue("id")
	o, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ORD_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	// Authorization: owner buyer OR verified ops/service JWT.
	c, ok := authn.ClaimsFromContext(r.Context())
	opsOK := ok && authn.IsOperatorOrService(c, "ops:order:recover")
	if !opsOK && ok && (c.PrincipalType == "operator" || c.PrincipalType == "service") &&
		(authn.HasScope(c, "ops:order:recover") || authn.HasScope(c, "*")) {
		opsOK = true
	}
	b := buyer(r)
	if !opsOK {
		if b == "" || o.BuyerID != b {
			httpx.WriteError(w, 403, "ORD_FORBIDDEN", "not owner; operator/service JWT required for cross-buyer reconcile", corr, "failed", false)
			return
		}
	}
	ctx := r.Context()
	if tok := bearerToken(r); tok != "" {
		ctx = orchestrate.WithServiceToken(ctx, tok)
	}
	out, err := s.Driver.Advance(ctx, o.ID, corr)
	if err != nil && out.ID == "" {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"order":  out,
		"result": reconcileResult(out.Status),
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
		return h[7:]
	}
	return ""
}

func reconcileResult(status string) string {
	switch status {
	case orchestrate.StatusConfirmed:
		return "resolved_confirmed"
	case orchestrate.StatusFailed:
		return "resolved_failed"
	case orchestrate.StatusPaymentPending, orchestrate.StatusReconciliationRequired:
		return "still_ambiguous"
	default:
		return "in_progress"
	}
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
		PaymentScenario string `json:"payment_scenario"`
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
		// Resume non-terminal orders; return terminals as-is.
		if existing.Status == orchestrate.StatusConfirmed || existing.Status == orchestrate.StatusFailed {
			httpx.WriteJSON(w, 200, existing)
			return
		}
		if req.PaymentScenario != "" {
			s.Driver.PaymentScenario = req.PaymentScenario
		}
		out, _ := s.Driver.Advance(r.Context(), existing.ID, corr)
		httpx.WriteJSON(w, 200, out)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
		return
	}

	// Policy snapshot (best-effort; default on failure is not used — require sale)
	policy, err := s.fetchPolicy(r.Context(), req.SessionID, corr)
	if err != nil {
		httpx.WriteError(w, 502, "ORD_SALE_UNAVAILABLE", err.Error(), corr, "failed", true)
		return
	}
	maxPerBuyer := intFrom(policy["max_per_buyer"], 4)
	policyVersion := intFrom(policy["version"], 1)

	now := time.Now().UTC()
	intendedID := ids.New("ord")
	o := store.Order{
		ID: intendedID, BuyerID: b, SessionID: req.SessionID, TicketTypeID: req.TicketTypeID,
		PoolID: req.PoolID, Quantity: req.Quantity, Status: orchestrate.StatusCreated, EntitlementID: req.EntitlementID,
		CheckoutOperationID: ids.New("chk"), InventoryOperationID: ids.New("invop"), PaymentOperationID: ids.New("payop"),
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
	if o.ID != intendedID {
		// Lost create race — resume winner
		if o.Status != orchestrate.StatusConfirmed && o.Status != orchestrate.StatusFailed {
			out, _ := s.Driver.Advance(r.Context(), o.ID, corr)
			httpx.WriteJSON(w, 200, out)
			return
		}
		httpx.WriteJSON(w, 200, o)
		return
	}

	if req.PaymentScenario != "" {
		s.Driver.PaymentScenario = req.PaymentScenario
	} else {
		s.Driver.PaymentScenario = s.PaymentScenario
	}
	out, err := s.Driver.Advance(r.Context(), o.ID, corr)
	if err != nil && out.Status == orchestrate.StatusFailed {
		httpx.WriteJSON(w, 200, out)
		return
	}
	status := 201
	if out.Status != orchestrate.StatusConfirmed {
		status = 200
	}
	httpx.WriteJSON(w, status, out)
}

func (s *Server) fetchPolicy(ctx context.Context, sessionID, corr string) (map[string]any, error) {
	ds := &orchestrate.HTTPDownstream{HTTP: s.HTTP, AdmissionBase: s.AdmissionBase, InventoryBase: s.InventoryBase, PaymentBase: s.PaymentBase}
	// reuse get via temporary method — inline small get
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.SaleBase+"/v1/sessions/"+sessionID+"/purchase-policy", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(httpx.HeaderCorrelationID, corr)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sale policy status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	_ = ds
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

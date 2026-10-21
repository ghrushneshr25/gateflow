package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"github.com/ghrushneshrathod/gateflow/services/payment-simulator/internal/store"
)

type PaymentStore interface {
	CreateOrGet(ctx context.Context, p store.Payment) (store.Payment, error)
	UpdateStatus(ctx context.Context, id, status string, now time.Time) (store.Payment, error)
	GetByOperation(ctx context.Context, op string) (store.Payment, error)
	ApplyCallback(ctx context.Context, operationID, status string, now time.Time) (store.Payment, error)
	ResolveDue(ctx context.Context, now time.Time, limit int) (int, error)
	CreateOrGetCompensation(ctx context.Context, c store.Compensation) (store.Compensation, error)
	GetCompensationByOperation(ctx context.Context, op string) (store.Compensation, error)
	GetCompensation(ctx context.Context, id string) (store.Compensation, error)
	ExecuteCompensationAttempt(ctx context.Context, operationID string, forceFail bool, now time.Time) (store.Compensation, error)
	MarkCompensationUnknown(ctx context.Context, operationID string, now time.Time) (store.Compensation, error)
}

type Server struct {
	Store  PaymentStore
	Signer *authn.DevSigner
}

func New(st PaymentStore) *Server {
	return &Server{
		Store:  st,
		Signer: authn.NewDevSigner(os.Getenv("GATEFLOW_DEV_JWT_SECRET")),
	}
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/payments", s.create)
	mux.HandleFunc("GET /v1/payments/by-operation/{operation_id}", s.byOp)
	mux.HandleFunc("POST /v1/payments/by-operation/{operation_id}/callbacks", s.callback)
	mux.HandleFunc("POST /v1/payments/resolve-due", s.resolveDue)
	mux.HandleFunc("POST /v1/compensations", s.createComp)
	mux.HandleFunc("GET /v1/compensations/by-operation/{operation_id}", s.getCompByOp)
	mux.HandleFunc("GET /v1/compensations/{id}", s.getComp)
	mux.HandleFunc("POST /v1/compensations/by-operation/{operation_id}/retry", s.retryComp)
}

func (s *Server) requireServiceOrOps(w http.ResponseWriter, r *http.Request, corr, scope string) bool {
	// Exact scope required (no OR across callback vs recover families).
	_, err := authn.RequireOperatorOrService(r, scope)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "PAY_UNAUTHORIZED", err.Error(), corr, "failed", false)
		return false
	}
	return true
}

func (s *Server) requireService(w http.ResponseWriter, r *http.Request, corr, scope string) bool {
	_, err := authn.RequireService(r, scope)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "PAY_UNAUTHORIZED", err.Error(), corr, "failed", false)
		return false
	}
	return true
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireService(w, r, corr, authn.ScopePaymentCreate) {
		return
	}
	var req struct {
		OperationID       string `json:"operation_id"`
		OrderID           string `json:"order_id"`
		AmountCents       int64  `json:"amount_cents"`
		Currency          string `json:"currency"`
		Scenario          string `json:"scenario"`
		DelaySeconds      int    `json:"delay_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OperationID == "" || req.OrderID == "" {
		httpx.WriteError(w, 400, "PAY_VALIDATION_FAILED", "invalid payment", corr, "failed", false)
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.Scenario == "" {
		req.Scenario = "success"
	}
	now := time.Now().UTC()
	status := "PENDING"
	var resolveAfter *time.Time
	switch req.Scenario {
	case "success":
		status = "PENDING" // will flip immediately below
	case "failure":
		status = "PENDING"
	case "delay_success":
		d := req.DelaySeconds
		if d <= 0 {
			d = 2
		}
		t := now.Add(time.Duration(d) * time.Second)
		resolveAfter = &t
		status = "PENDING"
	case "unknown":
		status = "UNKNOWN"
	default:
		status = "PENDING"
	}

	p, err := s.Store.CreateOrGet(r.Context(), store.Payment{
		ID: ids.New("pay"), OperationID: req.OperationID, OrderID: req.OrderID,
		AmountCents: req.AmountCents, Currency: req.Currency, Scenario: req.Scenario,
		Status: status, ResolveAfter: resolveAfter, CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, store.ErrIdempotency) {
		metrics.PaymentOutcome("error")
		httpx.WriteError(w, 409, "PAY_IDEMPOTENCY_CONFLICT", "conflict", corr, "conflict", false)
		return
	}
	if err != nil {
		metrics.PaymentOutcome("error")
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if p.Status == "SUCCEEDED" || p.Status == "FAILED" {
		metrics.PaymentOutcome(strings.ToLower(p.Status))
		httpx.WriteJSON(w, 200, p)
		return
	}
	// Immediate resolution for success/failure scenarios (not delay/unknown)
	if p.Scenario == "success" && p.Status == "PENDING" && p.ResolveAfter == nil {
		p, err = s.Store.UpdateStatus(r.Context(), p.ID, "SUCCEEDED", time.Now().UTC())
	} else if p.Scenario == "failure" && p.Status == "PENDING" {
		p, err = s.Store.UpdateStatus(r.Context(), p.ID, "FAILED", time.Now().UTC())
	}
	if err != nil {
		metrics.PaymentOutcome("error")
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	metrics.PaymentOutcome(paymentOutcomeLabel(p.Status))
	httpx.WriteJSON(w, 201, p)
}

func paymentOutcomeLabel(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCEEDED":
		return "succeeded"
	case "FAILED":
		return "failed"
	case "PENDING":
		return "pending"
	case "UNKNOWN":
		return "unknown"
	default:
		return "error"
	}
}

func (s *Server) byOp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	// Service or operator with create/recover may look up (order S2S uses payment:create).
	if _, err := authn.RequireOperatorOrService(r, authn.ScopePaymentCreate, authn.ScopePaymentRecover); err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "PAY_UNAUTHORIZED", err.Error(), corr, "failed", false)
		return
	}
	// Auto-resolve due delayed payments on lookup (deterministic without sleep in callers).
	_, _ = s.Store.ResolveDue(r.Context(), time.Now().UTC(), 50)
	p, err := s.Store.GetByOperation(r.Context(), r.PathValue("operation_id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "PAY_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, p)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireServiceOrOps(w, r, corr, "payment:callback") {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Status == "" {
		req.Status = "SUCCEEDED"
	}
	if req.Status != "SUCCEEDED" && req.Status != "FAILED" {
		httpx.WriteError(w, 400, "PAY_VALIDATION_FAILED", "status must be SUCCEEDED or FAILED", corr, "failed", false)
		return
	}
	p, err := s.Store.ApplyCallback(r.Context(), r.PathValue("operation_id"), req.Status, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "PAY_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, p)
}

func (s *Server) resolveDue(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireServiceOrOps(w, r, corr, authn.ScopePaymentRecover) {
		return
	}
	n, err := s.Store.ResolveDue(r.Context(), time.Now().UTC(), 200)
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"resolved": n})
}

func compensationOpID(paymentOperationID string) string {
	return "comp_" + paymentOperationID
}

func (s *Server) createComp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireServiceOrOps(w, r, corr, "ops:payment:recover") {
		return
	}
	var req struct {
		PaymentOperationID string `json:"payment_operation_id"`
		OrderID            string `json:"order_id"`
		Execute            bool   `json:"execute"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PaymentOperationID == "" || req.OrderID == "" {
		httpx.WriteError(w, 400, "PAY_VALIDATION_FAILED", "payment_operation_id and order_id required", corr, "failed", false)
		return
	}
	pay, err := s.Store.GetByOperation(r.Context(), req.PaymentOperationID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "PAY_NOT_FOUND", "payment not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	now := time.Now().UTC()
	opID := compensationOpID(req.PaymentOperationID)
	c, err := s.Store.CreateOrGetCompensation(r.Context(), store.Compensation{
		ID: ids.New("cmp"), OperationID: opID, PaymentID: pay.ID, OrderID: req.OrderID,
		Status: "CREATED", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if req.Execute && c.Status != "SUCCEEDED" {
		forceFail := strings.Contains(pay.Scenario, "compensation_fail")
		c, err = s.Store.ExecuteCompensationAttempt(r.Context(), opID, forceFail, time.Now().UTC())
		if err != nil {
			metrics.CompensationOutcome("error")
			httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
			return
		}
	}
	metrics.CompensationOutcome(compensationOutcomeLabel(c.Status))
	httpx.WriteJSON(w, 200, c)
}

func compensationOutcomeLabel(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCEEDED":
		return "succeeded"
	case "FAILED":
		return "failed"
	case "CREATED":
		return "created"
	default:
		return "error"
	}
}

func (s *Server) getCompByOp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireServiceOrOps(w, r, corr, "ops:payment:recover") {
		return
	}
	c, err := s.Store.GetCompensationByOperation(r.Context(), r.PathValue("operation_id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "PAY_COMP_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, c)
}

func (s *Server) getComp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireServiceOrOps(w, r, corr, "ops:payment:recover") {
		return
	}
	c, err := s.Store.GetCompensation(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "PAY_COMP_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, c)
}

func (s *Server) retryComp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireServiceOrOps(w, r, corr, "ops:payment:recover") {
		return
	}
	op := r.PathValue("operation_id")
	// Query first if UNKNOWN
	existing, err := s.Store.GetCompensationByOperation(r.Context(), op)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "PAY_COMP_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if existing.Status == "SUCCEEDED" {
		httpx.WriteJSON(w, 200, existing)
		return
	}
	if existing.Status == "UNKNOWN" {
		// Re-query same operation (already loaded) before executing another attempt.
		httpx.WriteJSON(w, 200, map[string]any{
			"compensation": existing,
			"action":       "queried_ambiguous_before_retry",
			"hint":         "call retry again with X-Force-Retry: 1 to attempt execution",
		})
		if r.Header.Get("X-Force-Retry") != "1" {
			return
		}
	}
	// Determine forceFail from linked payment scenario only on first failure path
	forceFail := false
	c, err := s.Store.ExecuteCompensationAttempt(r.Context(), op, forceFail, time.Now().UTC())
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, c)
}

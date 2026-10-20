package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/payment-simulator/internal/store"
)

type PaymentStore interface {
	CreateOrGet(ctx context.Context, p store.Payment) (store.Payment, error)
	UpdateStatus(ctx context.Context, id, status string, now time.Time) (store.Payment, error)
	GetByOperation(ctx context.Context, op string) (store.Payment, error)
}

type Server struct{ Store PaymentStore }

func New(st PaymentStore) *Server { return &Server{Store: st} }

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/payments", s.create)
	mux.HandleFunc("GET /v1/payments/by-operation/{operation_id}", s.byOp)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	var req struct {
		OperationID string `json:"operation_id"`
		OrderID     string `json:"order_id"`
		AmountCents int64  `json:"amount_cents"`
		Currency    string `json:"currency"`
		Scenario    string `json:"scenario"`
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
	p, err := s.Store.CreateOrGet(r.Context(), store.Payment{
		ID: ids.New("pay"), OperationID: req.OperationID, OrderID: req.OrderID,
		AmountCents: req.AmountCents, Currency: req.Currency, Scenario: req.Scenario,
		Status: "PENDING", CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, store.ErrIdempotency) {
		httpx.WriteError(w, 409, "PAY_IDEMPOTENCY_CONFLICT", "conflict", corr, "conflict", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	// Already terminal?
	if p.Status == "SUCCEEDED" || p.Status == "FAILED" {
		httpx.WriteJSON(w, 200, p)
		return
	}
	status := "SUCCEEDED"
	if req.Scenario == "failure" || p.Scenario == "failure" {
		status = "FAILED"
	}
	p, err = s.Store.UpdateStatus(r.Context(), p.ID, status, time.Now().UTC())
	if err != nil {
		httpx.WriteError(w, 500, "PAY_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, p)
}

func (s *Server) byOp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
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

package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/inventory/internal/store"
)

// InventoryStore is the persistence port (mockable via nexus).
type InventoryStore interface {
	UpsertPool(ctx context.Context, p store.Pool) (store.Pool, error)
	GetPool(ctx context.Context, id string) (store.Pool, error)
	Reserve(ctx context.Context, res store.Reservation) (store.Reservation, error)
	GetByOperation(ctx context.Context, operationID string) (store.Reservation, error)
	GetReservation(ctx context.Context, id string) (store.Reservation, error)
	Confirm(ctx context.Context, id string, now time.Time) (store.Reservation, error)
	Release(ctx context.Context, id string, now time.Time) (store.Reservation, error)
	Expire(ctx context.Context, id string, now time.Time) (store.Reservation, error)
	ExpireDue(ctx context.Context, now time.Time, limit int) (int, error)
}

type Server struct{ Store InventoryStore }

func New(st InventoryStore) *Server { return &Server{Store: st} }

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/pools", s.createPool)
	mux.HandleFunc("GET /v1/pools/{id}", s.getPool)
	mux.HandleFunc("POST /v1/reservations", s.reserve)
	mux.HandleFunc("GET /v1/reservations/by-operation/{operation_id}", s.byOp)
	mux.HandleFunc("GET /v1/reservations/{id}", s.getRes)
	mux.HandleFunc("POST /v1/reservations/{id}/confirm", s.confirm)
	mux.HandleFunc("POST /v1/reservations/{id}/release", s.release)
	mux.HandleFunc("POST /v1/reservations/{id}/expire", s.expire)
	mux.HandleFunc("POST /v1/reservations/expire-due", s.expireDue)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	var req struct {
		SessionID    string `json:"session_id"`
		TicketTypeID string `json:"ticket_type_id"`
		Name         string `json:"name"`
		Capacity     int    `json:"capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TicketTypeID == "" || req.Capacity < 0 {
		httpx.WriteError(w, 400, "INV_VALIDATION_FAILED", "invalid pool", corr, "failed", false)
		return
	}
	p, err := s.Store.UpsertPool(r.Context(), store.Pool{
		ID: ids.New("pool"), SessionID: req.SessionID, TicketTypeID: req.TicketTypeID,
		Name: req.Name, SellableCapacity: req.Capacity, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, p)
}

func (s *Server) getPool(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	p, err := s.Store.GetPool(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "pool not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, p)
}

func (s *Server) reserve(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	var req struct {
		OperationID  string `json:"operation_id"`
		PoolID       string `json:"pool_id"`
		Quantity     int    `json:"quantity"`
		BuyerID      string `json:"buyer_id"`
		OrderID      string `json:"order_id"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OperationID == "" || req.PoolID == "" || req.Quantity <= 0 {
		httpx.WriteError(w, 400, "INV_VALIDATION_FAILED", "invalid reservation", corr, "failed", false)
		return
	}
	if req.LeaseSeconds <= 0 {
		req.LeaseSeconds = 300
	}
	if req.LeaseSeconds < 60 {
		req.LeaseSeconds = 60
	}
	if req.LeaseSeconds > 900 {
		req.LeaseSeconds = 900
	}
	now := time.Now().UTC()
	res, err := s.Store.Reserve(r.Context(), store.Reservation{
		ID: ids.New("res"), OperationID: req.OperationID, PoolID: req.PoolID,
		Quantity: req.Quantity, BuyerID: req.BuyerID, OrderID: req.OrderID,
		ExpiresAt: now.Add(time.Duration(req.LeaseSeconds) * time.Second),
		CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, store.ErrCapacityExceeded) {
		httpx.WriteError(w, 409, "INV_CAPACITY_EXCEEDED", "insufficient capacity", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrIdempotency) {
		httpx.WriteError(w, 409, "INV_IDEMPOTENCY_CONFLICT", "conflicting operation", corr, "conflict", false)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "pool not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, res)
}

func (s *Server) byOp(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	res, err := s.Store.GetByOperation(r.Context(), r.PathValue("operation_id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "reservation not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, res)
}

func (s *Server) getRes(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	res, err := s.Store.GetReservation(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "reservation not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, res)
}

func (s *Server) confirm(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	res, err := s.Store.Confirm(r.Context(), r.PathValue("id"), time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "reservation not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrExpired) {
		httpx.WriteError(w, 409, "INV_RESERVATION_EXPIRED", "reservation expired", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrNotActive) {
		httpx.WriteError(w, 409, "INV_RESERVATION_NOT_ACTIVE", "reservation not active", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, res)
}

func (s *Server) release(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	res, err := s.Store.Release(r.Context(), r.PathValue("id"), time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "reservation not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrNotActive) {
		httpx.WriteError(w, 409, "INV_RESERVATION_NOT_ACTIVE", "cannot release", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, res)
}

func (s *Server) expire(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	res, err := s.Store.Expire(r.Context(), r.PathValue("id"), time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "INV_NOT_FOUND", "reservation not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrNotActive) {
		httpx.WriteError(w, 409, "INV_RESERVATION_NOT_ACTIVE", "cannot expire", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, res)
}

func (s *Server) expireDue(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	n, err := s.Store.ExpireDue(r.Context(), time.Now().UTC(), 200)
	if err != nil {
		httpx.WriteError(w, 500, "INV_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"expired": n})
}

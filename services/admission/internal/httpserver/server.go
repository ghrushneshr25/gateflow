package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

type AdmissionStore interface {
	Join(ctx context.Context, entry store.QueueEntry) (store.QueueEntry, error)
	GetMine(ctx context.Context, sessionID, buyerID string) (store.QueueEntry, error)
	GrantFIFO(ctx context.Context, sessionID, buyerID, entitlementID string, unboundTTL time.Duration, now time.Time) (store.QueueEntry, store.Entitlement, error)
	GetEntitlement(ctx context.Context, id string) (store.Entitlement, error)
	Bind(ctx context.Context, id, buyerID, checkoutOp string, now time.Time) (store.Entitlement, error)
	Consume(ctx context.Context, id, checkoutOp string, now time.Time) (store.Entitlement, error)
	EnsureBudgets(ctx context.Context, sessionID string, maxGrants, maxActive int) error
	GetBudget(ctx context.Context, scopeKey string) (store.Budget, error)
	SetBudgetCaps(ctx context.Context, scopeKey string, maxGrants, maxActive int) error
	GetCoordination(ctx context.Context) (store.Coordination, error)
	FenceAdmission(ctx context.Context, reason, actor string, now time.Time) (store.Coordination, error)
	UnfenceAdmission(ctx context.Context, actor string, now time.Time) (store.Coordination, error)
}

type Server struct {
	Store            AdmissionStore
	FencedGrantTotal atomic.Int64
}

func New(st AdmissionStore) *Server { return &Server{Store: st} }

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/sessions/{session_id}/queue/joins", s.join)
	mux.HandleFunc("GET /v1/sessions/{session_id}/queue/me", s.me)
	mux.HandleFunc("POST /v1/sessions/{session_id}/grants", s.grant)
	mux.HandleFunc("GET /v1/entitlements/{id}", s.getEnt)
	mux.HandleFunc("POST /v1/entitlements/{id}/bind", s.bind)
	mux.HandleFunc("POST /v1/entitlements/{id}/consume", s.consume)
	mux.HandleFunc("GET /v1/admin/budgets/{scope}", s.getBudget)
	mux.HandleFunc("PUT /v1/admin/budgets/{scope}", s.putBudget)
	mux.HandleFunc("GET /v1/admin/coordination", s.getCoordination)
	mux.HandleFunc("POST /v1/admin/coordination/fence", s.fence)
	mux.HandleFunc("POST /v1/admin/coordination/unfence", s.unfence)
	mux.HandleFunc("GET /v1/admin/coordination/metrics", s.coordMetrics)
}

func (s *Server) requireOps(w http.ResponseWriter, r *http.Request, corr, scope string) (authn.Claims, bool) {
	scopes := []string{scope}
	if scope == authn.ScopeAdmissionRead {
		scopes = []string{authn.ScopeAdmissionRead, authn.ScopeAdmissionFence, authn.ScopeAdmissionBudget}
	}
	c, err := authn.RequireOperatorOrService(r, scopes...)
	if err != nil {
		code := "ADM_UNAUTHORIZED"
		if errors.Is(err, authn.ErrForbidden) {
			code = "ADM_FORBIDDEN"
		}
		httpx.WriteError(w, authn.StatusCode(err), code, err.Error(), corr, "failed", false)
		return c, false
	}
	return c, true
}

func (s *Server) requireService(w http.ResponseWriter, r *http.Request, corr string, scopes ...string) bool {
	_, err := authn.RequireService(r, scopes...)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "ADM_UNAUTHORIZED", err.Error(), corr, "failed", false)
		return false
	}
	return true
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	bc, err := authn.RequireBuyer(r)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "ADM_UNAUTHORIZED", "buyer JWT required", corr, "failed", false)
		return
	}
	buyer := authn.BuyerID(bc)
	// Drain body if present (buyer_id in body is ignored).
	var body struct {
		BuyerID string `json:"buyer_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	entry, err := s.Store.Join(r.Context(), store.QueueEntry{
		ID: ids.New("q"), SessionID: r.PathValue("session_id"), BuyerID: buyer, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, entry)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	bc, err := authn.RequireBuyer(r)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "ADM_UNAUTHORIZED", "buyer JWT required", corr, "failed", false)
		return
	}
	buyer := authn.BuyerID(bc)
	entry, err := s.Store.GetMine(r.Context(), r.PathValue("session_id"), buyer)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ADM_NOT_FOUND", "not in queue", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, entry)
}

func (s *Server) grant(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	var req struct {
		BuyerID string `json:"buyer_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Authorize, then always use the same GrantFIFO store path (no alternate entitlement write).
	var buyerID string
	if _, err := authn.RequireService(r, authn.ScopeAdmissionGrant); err == nil {
		// Authorized service may grant for an explicit target buyer.
		buyerID = req.BuyerID
		if buyerID == "" {
			httpx.WriteError(w, 400, "ADM_VALIDATION_FAILED", "buyer_id required", corr, "failed", false)
			return
		}
	} else if bc, berr := authn.RequireBuyer(r); berr == nil {
		// Buyer self-grant only: verified JWT sub. Body/header buyer_id is ignored.
		buyerID = authn.BuyerID(bc)
	} else {
		// Prefer the first auth error (service path) when both fail — typically missing/invalid token.
		httpx.WriteError(w, authn.StatusCode(err), "ADM_UNAUTHORIZED", "service (admission:grant) or buyer JWT required", corr, "failed", false)
		return
	}

	sessionID := r.PathValue("session_id")
	_ = s.Store.EnsureBudgets(r.Context(), sessionID, 10000, 1000)
	// Authoritative grant: queue row lock, fence, budget consume, entitlement insert, idempotent re-read.
	entry, ent, err := s.Store.GrantFIFO(r.Context(), sessionID, buyerID, ids.New("ent"), 10*time.Minute, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		metrics.AdmissionGrant("not_found")
		httpx.WriteError(w, 404, "ADM_NOT_FOUND", "queue entry not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrBudgetExhausted) {
		metrics.AdmissionGrant("budget")
		httpx.WriteError(w, 429, "ADM_BUDGET_EXHAUSTED", "admission grant budget exhausted", corr, "failed", true)
		return
	}
	if errors.Is(err, store.ErrCoordinationFenced) {
		s.FencedGrantTotal.Add(1)
		metrics.AdmissionGrant("fenced")
		httpx.WriteError(w, 503, "ADM_COORDINATION_FENCED", "new grants are fenced", corr, "failed", true)
		return
	}
	if errors.Is(err, store.ErrCoordinationUnavailable) {
		metrics.AdmissionGrant("error")
		httpx.WriteError(w, 503, "ADM_COORDINATION_UNAVAILABLE", "coordination state unavailable", corr, "failed", true)
		return
	}
	if err != nil {
		metrics.AdmissionGrant("error")
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	metrics.AdmissionGrant("ok")
	httpx.WriteJSON(w, 200, map[string]any{"queue_entry": entry, "entitlement": ent})
}

func (s *Server) getCoordination(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if _, ok := s.requireOps(w, r, corr, "ops:admission:read"); !ok {
		return
	}
	c, err := s.Store.GetCoordination(r.Context())
	if errors.Is(err, store.ErrCoordinationUnavailable) {
		httpx.WriteError(w, 503, "ADM_COORDINATION_UNAVAILABLE", "missing coordination state", corr, "failed", true)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	metrics.SetAdmissionFenced(c.Mode == store.CoordModeFenced, float64(c.Epoch))
	httpx.WriteJSON(w, 200, c)
}

func (s *Server) fence(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	claims, ok := s.requireOps(w, r, corr, "ops:admission:fence")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	c, err := s.Store.FenceAdmission(r.Context(), req.Reason, claims.Subject, time.Now().UTC())
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	metrics.SetAdmissionFenced(c.Mode == store.CoordModeFenced, float64(c.Epoch))
	httpx.WriteJSON(w, 200, c)
}

func (s *Server) unfence(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	claims, ok := s.requireOps(w, r, corr, "ops:admission:fence")
	if !ok {
		return
	}
	c, err := s.Store.UnfenceAdmission(r.Context(), claims.Subject, time.Now().UTC())
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	metrics.SetAdmissionFenced(c.Mode == store.CoordModeFenced, float64(c.Epoch))
	httpx.WriteJSON(w, 200, c)
}

func (s *Server) coordMetrics(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if _, ok := s.requireOps(w, r, corr, "ops:admission:read"); !ok {
		return
	}
	c, err := s.Store.GetCoordination(r.Context())
	mode := "unknown"
	epoch := int64(0)
	if err == nil {
		mode = c.Mode
		epoch = c.Epoch
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"coordination_mode":  mode,
		"coordination_epoch": epoch,
		"fenced_grant_total": s.FencedGrantTotal.Load(),
	})
}

func (s *Server) getEnt(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	ent, err := s.Store.GetEntitlement(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ADM_ENTITLEMENT_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	// Service may read any entitlement; buyer only own.
	if _, err := authn.RequireService(r, authn.ScopeAdmissionEntitlement, authn.ScopeAdmissionGrant); err == nil {
		httpx.WriteJSON(w, 200, ent)
		return
	}
	bc, err := authn.RequireBuyer(r)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "ADM_UNAUTHORIZED", "buyer or service JWT required", corr, "failed", false)
		return
	}
	if ent.BuyerID != authn.BuyerID(bc) {
		httpx.WriteError(w, 403, "ADM_FORBIDDEN", "not owner", corr, "failed", false)
		return
	}
	httpx.WriteJSON(w, 200, ent)
}

func (s *Server) bind(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireService(w, r, corr, authn.ScopeAdmissionEntitlement) {
		return
	}
	var req struct {
		BuyerID             string `json:"buyer_id"`
		CheckoutOperationID string `json:"checkout_operation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BuyerID == "" || req.CheckoutOperationID == "" {
		httpx.WriteError(w, 400, "ADM_VALIDATION_FAILED", "buyer_id and checkout_operation_id required", corr, "failed", false)
		return
	}
	ent, err := s.Store.Bind(r.Context(), r.PathValue("id"), req.BuyerID, req.CheckoutOperationID, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ADM_ENTITLEMENT_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrExpired) {
		httpx.WriteError(w, 409, "ADM_ENTITLEMENT_EXPIRED", "expired", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrBoundOther) {
		httpx.WriteError(w, 409, "ADM_ENTITLEMENT_BOUND_OTHER", "bound to other checkout", corr, "conflict", false)
		return
	}
	if errors.Is(err, store.ErrBudgetExhausted) {
		httpx.WriteError(w, 429, "ADM_BUDGET_EXHAUSTED", "active checkout budget exhausted", corr, "failed", true)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, ent)
}

func (s *Server) getBudget(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if _, err := authn.RequireOperatorOrService(r, authn.ScopeAdmissionBudget, authn.ScopeAdmissionRead, authn.ScopeAdmissionFence); err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "ADM_UNAUTHORIZED", err.Error(), corr, "failed", false)
		return
	}
	scope := r.PathValue("scope")
	if scope != "global" && !strings.HasPrefix(scope, "session:") {
		scope = "session:" + scope
	}
	b, err := s.Store.GetBudget(r.Context(), scope)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ADM_NOT_FOUND", "budget scope not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, b)
}

func (s *Server) putBudget(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if _, ok := s.requireOps(w, r, corr, authn.ScopeAdmissionBudget); !ok {
		return
	}
	scope := r.PathValue("scope")
	if scope != "global" {
		scope = "session:" + strings.TrimPrefix(scope, "session:")
	}
	var req struct {
		MaxGrants          int `json:"max_grants"`
		MaxActiveCheckouts int `json:"max_active_checkouts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "ADM_VALIDATION_FAILED", "invalid body", corr, "failed", false)
		return
	}
	// Caps only — does not reset grants_issued or active_checkouts (no blind reset).
	if err := s.Store.SetBudgetCaps(r.Context(), scope, req.MaxGrants, req.MaxActiveCheckouts); err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	b, err := s.Store.GetBudget(r.Context(), scope)
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, b)
}

func (s *Server) consume(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireService(w, r, corr, authn.ScopeAdmissionEntitlement) {
		return
	}
	var req struct {
		CheckoutOperationID string `json:"checkout_operation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CheckoutOperationID == "" {
		httpx.WriteError(w, 400, "ADM_VALIDATION_FAILED", "checkout_operation_id required", corr, "failed", false)
		return
	}
	ent, err := s.Store.Consume(r.Context(), r.PathValue("id"), req.CheckoutOperationID, time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "ADM_ENTITLEMENT_NOT_FOUND", "not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrInvalidState) {
		httpx.WriteError(w, 409, "ADM_INVALID_STATE", "cannot consume", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "ADM_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, ent)
}

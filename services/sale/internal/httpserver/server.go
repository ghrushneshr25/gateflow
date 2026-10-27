package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
)

func (s *Server) requireOps(w http.ResponseWriter, r *http.Request, corr string, scopes ...string) bool {
	_, err := authn.RequireOperator(r, scopes...)
	if err != nil {
		httpx.WriteError(w, authn.StatusCode(err), "SALE_UNAUTHORIZED", err.Error(), corr, "failed", false)
		return false
	}
	return true
}

func serviceToken() string {
	if t := os.Getenv("GATEFLOW_SERVICE_TOKEN"); t != "" {
		return t
	}
	tok, err := authn.MintServiceJWT(os.Getenv("GATEFLOW_DEV_JWT_SECRET"), "sale-service", authn.SaleServiceScopes, 24*time.Hour)
	if err != nil {
		return ""
	}
	return tok
}

type SaleStore interface {
	CreateOffering(ctx context.Context, o store.Offering) error
	GetOffering(ctx context.Context, id string) (store.Offering, error)
	ListOfferings(ctx context.Context, publishedOnly bool) ([]store.Offering, error)
	CreateSession(ctx context.Context, sess store.Session, policy store.PurchasePolicy) error
	ListSessions(ctx context.Context, offeringID string) ([]store.Session, error)
	GetSession(ctx context.Context, id string) (store.Session, error)
	CreateTicketType(ctx context.Context, tt store.TicketType) error
	ListTicketTypes(ctx context.Context, sessionID string) ([]store.TicketType, error)
	SetTicketPoolID(ctx context.Context, ticketTypeID, poolID string) error
	LatestPolicy(ctx context.Context, sessionID string) (store.PurchasePolicy, error)
	PublishOffering(ctx context.Context, offeringID string, now time.Time) (store.Offering, []store.Session, []store.TicketType, error)
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Server struct {
	Store            SaleStore
	InventoryBaseURL string
	HTTP             HTTPDoer
	// googleValidate optional; nil uses Google idtoken.Validate.
	googleValidate googleIDTokenValidator
}

func New(st *store.Store) *Server {
	base := os.Getenv("INVENTORY_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:8083"
	}
	return NewWithDeps(st, httpx.NewClient(5*time.Second), base)
}

func NewWithDeps(st SaleStore, httpDoer HTTPDoer, inventoryBase string) *Server {
	if httpDoer == nil {
		httpDoer = httpx.NewClient(5 * time.Second)
	}
	if inventoryBase == "" {
		inventoryBase = "http://127.0.0.1:8083"
	}
	return &Server{Store: st, InventoryBaseURL: inventoryBase, HTTP: httpDoer}
}

func (s *Server) Routes(mux *http.ServeMux) {
	s.mountAuthRoutes(mux)
	s.mountBuyerAuthRoutes(mux)
	mux.HandleFunc("POST /v1/offerings", s.createOffering)
	mux.HandleFunc("GET /v1/offerings", s.listOfferings)
	mux.HandleFunc("GET /v1/offerings/{id}", s.getOffering)
	mux.HandleFunc("POST /v1/offerings/{id}/sessions", s.createSession)
	mux.HandleFunc("GET /v1/offerings/{id}/sessions", s.listSessions)
	mux.HandleFunc("POST /v1/sessions/{id}/ticket-types", s.createTicketType)
	mux.HandleFunc("GET /v1/sessions/{id}/ticket-types", s.listTicketTypes)
	mux.HandleFunc("GET /v1/sessions/{id}/purchase-policy", s.getPolicy)
	mux.HandleFunc("POST /v1/offerings/{id}/publish", s.publish)
}

func (s *Server) createOffering(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOps(w, r, corr, authn.ScopeSaleWrite) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpx.WriteError(w, 400, "SALE_VALIDATION_FAILED", "name is required", corr, "failed", false)
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	now := time.Now().UTC()
	o := store.Offering{
		ID: ids.New("off"), Name: req.Name, Description: req.Description,
		Category: req.Category, Status: "DRAFT", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Store.CreateOffering(r.Context(), o); err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, o)
}

func (s *Server) listOfferings(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	publishedOnly := r.URL.Query().Get("status") == "PUBLISHED" || r.URL.Query().Get("buyer") == "1"
	items, err := s.Store.ListOfferings(r.Context(), publishedOnly)
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if items == nil {
		items = []store.Offering{}
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) getOffering(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	o, err := s.Store.GetOffering(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "SALE_NOT_FOUND", "offering not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, o)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOps(w, r, corr, authn.ScopeSaleWrite) {
		return
	}
	offeringID := r.PathValue("id")
	if _, err := s.Store.GetOffering(r.Context(), offeringID); errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "SALE_NOT_FOUND", "offering not found", corr, "failed", false)
		return
	}
	var req struct {
		StartsAt          time.Time `json:"starts_at"`
		EndsAt            time.Time `json:"ends_at"`
		SaleOpensAt       time.Time `json:"sale_opens_at"`
		SaleClosesAt      time.Time `json:"sale_closes_at"`
		AdmissionStrategy string    `json:"admission_strategy"`
		MaxPerBuyer       int       `json:"max_per_buyer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "SALE_VALIDATION_FAILED", "invalid body", corr, "failed", false)
		return
	}
	if req.EndsAt.Before(req.StartsAt) || req.SaleClosesAt.Before(req.SaleOpensAt) {
		httpx.WriteError(w, 400, "SALE_VALIDATION_FAILED", "invalid time windows", corr, "failed", false)
		return
	}
	if req.AdmissionStrategy == "" {
		req.AdmissionStrategy = "fifo"
	}
	if req.MaxPerBuyer <= 0 {
		req.MaxPerBuyer = 4
	}
	now := time.Now().UTC()
	sess := store.Session{
		ID: ids.New("ses"), OfferingID: offeringID,
		StartsAt: req.StartsAt, EndsAt: req.EndsAt,
		SaleOpensAt: req.SaleOpensAt, SaleClosesAt: req.SaleClosesAt,
		Status: "SCHEDULED", AdmissionStrategy: req.AdmissionStrategy, CreatedAt: now,
	}
	policy := store.PurchasePolicy{
		ID: ids.New("pol"), SessionID: sess.ID, Version: 1, MaxPerBuyer: req.MaxPerBuyer, CreatedAt: now,
	}
	if err := s.Store.CreateSession(r.Context(), sess, policy); err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, map[string]any{"session": sess, "purchase_policy": policy})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	items, err := s.Store.ListSessions(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if items == nil {
		items = []store.Session{}
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) createTicketType(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOps(w, r, corr, authn.ScopeSaleWrite) {
		return
	}
	sessionID := r.PathValue("id")
	if _, err := s.Store.GetSession(r.Context(), sessionID); errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "SALE_NOT_FOUND", "session not found", corr, "failed", false)
		return
	}
	var req struct {
		Name       string `json:"name"`
		Section    string `json:"section"`
		Capacity   int    `json:"capacity"`
		PriceCents int64  `json:"price_cents"`
		Currency   string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Capacity <= 0 {
		httpx.WriteError(w, 400, "SALE_VALIDATION_FAILED", "name and positive capacity required", corr, "failed", false)
		return
	}
	if req.Section == "" {
		req.Section = "GA"
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	tt := store.TicketType{
		ID: ids.New("tt"), SessionID: sessionID, Name: req.Name, Section: req.Section,
		Capacity: req.Capacity, PriceCents: req.PriceCents, Currency: req.Currency, CreatedAt: time.Now().UTC(),
	}
	if err := s.Store.CreateTicketType(r.Context(), tt); err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, tt)
}

func (s *Server) listTicketTypes(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	items, err := s.Store.ListTicketTypes(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if items == nil {
		items = []store.TicketType{}
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	p, err := s.Store.LatestPolicy(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "SALE_NOT_FOUND", "policy not found", corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, p)
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOps(w, r, corr, authn.ScopeSalePublish, authn.ScopeSaleWrite) {
		return
	}
	o, sessions, tts, err := s.Store.PublishOffering(r.Context(), r.PathValue("id"), time.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, 404, "SALE_NOT_FOUND", "offering not found", corr, "failed", false)
		return
	}
	if errors.Is(err, store.ErrConflict) {
		httpx.WriteError(w, 409, "SALE_INVALID_TRANSITION", err.Error(), corr, "failed", false)
		return
	}
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}

	// Ensure inventory pools exist for each ticket type.
	for i := range tts {
		if tts[i].InventoryPoolID != nil && *tts[i].InventoryPoolID != "" {
			continue
		}
		poolID, err := s.ensurePool(r.Context(), tts[i], corr)
		if err != nil {
			httpx.WriteError(w, 502, "SALE_INVENTORY_SYNC_FAILED", err.Error(), corr, "failed", true)
			return
		}
		if err := s.Store.SetTicketPoolID(r.Context(), tts[i].ID, poolID); err != nil {
			httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
			return
		}
		tts[i].InventoryPoolID = &poolID
	}

	httpx.WriteJSON(w, 200, map[string]any{
		"offering":     o,
		"sessions":     sessions,
		"ticket_types": tts,
	})
}

func (s *Server) ensurePool(ctx context.Context, tt store.TicketType, corr string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"session_id":    tt.SessionID,
		"ticket_type_id": tt.ID,
		"name":          tt.Name,
		"capacity":      tt.Capacity,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.InventoryBaseURL+"/v1/pools", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderCorrelationID, corr)
	if tok := serviceToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", errors.New(string(b))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("inventory returned empty pool id")
	}
	return out.ID, nil
}

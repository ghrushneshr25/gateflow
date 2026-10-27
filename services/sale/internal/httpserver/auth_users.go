package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
)

// Default operator scopes for bootstrap admin and new users (unless overridden).
var defaultOperatorScopes = []string{
	authn.ScopeSaleWrite,
	authn.ScopeSalePublish,
	authn.ScopeUsersAdmin,
	authn.ScopeOrderRecover,
	authn.ScopeOrderRead,
	authn.ScopeOutboxRead,
	authn.ScopeOutboxReplay,
	authn.ScopeAdmissionRead,
	authn.ScopeAdmissionFence,
	authn.ScopeAdmissionBudget,
	authn.ScopePaymentRecover,
}

type OperatorUserStore interface {
	CreateOperatorUser(ctx context.Context, u store.OperatorUser) error
	GetOperatorUserByUsername(ctx context.Context, username string) (store.OperatorUser, error)
	ListOperatorUsers(ctx context.Context) ([]store.OperatorUser, error)
	CountOperatorUsers(ctx context.Context) (int, error)
}

func (s *Server) mountAuthRoutes(mux *http.ServeMux) {
	// Operator username/password (admin console only).
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("POST /v1/auth/operator/login", s.login)
	mux.HandleFunc("POST /v1/admin/users", s.createUser)
	mux.HandleFunc("GET /v1/admin/users", s.listUsers)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "SALE_BAD_REQUEST", "invalid json", corr, "failed", false)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		httpx.WriteError(w, 400, "SALE_BAD_REQUEST", "username and password required", corr, "failed", false)
		return
	}
	us, ok := s.Store.(OperatorUserStore)
	if !ok {
		httpx.WriteError(w, 503, "SALE_UNAVAILABLE", "user store unavailable", corr, "failed", true)
		return
	}
	u, err := us.GetOperatorUserByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteError(w, 401, "SALE_AUTH_FAILED", "invalid username or password", corr, "failed", false)
			return
		}
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if !u.Active {
		httpx.WriteError(w, 403, "SALE_USER_DISABLED", "user disabled", corr, "failed", false)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		httpx.WriteError(w, 401, "SALE_AUTH_FAILED", "invalid username or password", corr, "failed", false)
		return
	}
	secret := os.Getenv("GATEFLOW_DEV_JWT_SECRET")
	tok, err := authn.MintOperatorJWT(secret, u.Username, u.Scopes, 8*time.Hour)
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{
		"access_token": tok,
		"token_type":   "Bearer",
		"expires_in":   8 * 3600,
		"user": map[string]any{
			"id":           u.ID,
			"username":     u.Username,
			"display_name": u.DisplayName,
			"scopes":       u.Scopes,
		},
	})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOps(w, r, corr, authn.ScopeUsersAdmin) {
		return
	}
	var req struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		DisplayName string   `json:"display_name"`
		Scopes      []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, 400, "SALE_BAD_REQUEST", "invalid json", corr, "failed", false)
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if err := validateUsername(req.Username); err != nil {
		httpx.WriteError(w, 400, "SALE_BAD_REQUEST", err.Error(), corr, "failed", false)
		return
	}
	if err := validatePassword(req.Password); err != nil {
		httpx.WriteError(w, 400, "SALE_BAD_REQUEST", err.Error(), corr, "failed", false)
		return
	}
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = append([]string{}, defaultOperatorScopes...)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	us, ok := s.Store.(OperatorUserStore)
	if !ok {
		httpx.WriteError(w, 503, "SALE_UNAVAILABLE", "user store unavailable", corr, "failed", true)
		return
	}
	now := time.Now().UTC()
	u := store.OperatorUser{
		ID:           ids.New("ops"),
		Username:     req.Username,
		PasswordHash: string(hash),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Scopes:       scopes,
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := us.CreateOperatorUser(r.Context(), u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			httpx.WriteError(w, 409, "SALE_USER_EXISTS", "username already taken", corr, "conflict", false)
			return
		}
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	httpx.WriteJSON(w, 201, map[string]any{
		"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
		"scopes": u.Scopes, "active": u.Active, "created_at": u.CreatedAt,
	})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	if !s.requireOps(w, r, corr, authn.ScopeUsersAdmin) {
		return
	}
	us, ok := s.Store.(OperatorUserStore)
	if !ok {
		httpx.WriteError(w, 503, "SALE_UNAVAILABLE", "user store unavailable", corr, "failed", true)
		return
	}
	items, err := us.ListOperatorUsers(r.Context())
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}
	if items == nil {
		items = []store.OperatorUser{}
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

// BootstrapDefaultAdmin creates the initial admin if no operator users exist.
func BootstrapDefaultAdmin(ctx context.Context, st OperatorUserStore) (created bool, username string, err error) {
	n, err := st.CountOperatorUsers(ctx)
	if err != nil {
		return false, "", err
	}
	if n > 0 {
		return false, "", nil
	}
	user := strings.TrimSpace(os.Getenv("GATEFLOW_ADMIN_USERNAME"))
	if user == "" {
		user = "admin"
	}
	user = strings.ToLower(user)
	pass := os.Getenv("GATEFLOW_ADMIN_PASSWORD")
	if pass == "" {
		pass = "GateflowAdmin1!"
	}
	if err := validateUsername(user); err != nil {
		return false, "", err
	}
	if err := validatePassword(pass); err != nil {
		return false, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return false, "", err
	}
	now := time.Now().UTC()
	u := store.OperatorUser{
		ID:           ids.New("ops"),
		Username:     user,
		PasswordHash: string(hash),
		DisplayName:  "Default Admin",
		Scopes:       append([]string{}, defaultOperatorScopes...),
		Active:       true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := st.CreateOperatorUser(ctx, u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return false, user, nil
		}
		return false, "", err
	}
	return true, user, nil
}

func validateUsername(u string) error {
	if len(u) < 3 || len(u) > 64 {
		return errors.New("username must be 3–64 characters")
	}
	for _, r := range u {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return errors.New("username may contain letters, digits, _ - . only")
		}
	}
	return nil
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

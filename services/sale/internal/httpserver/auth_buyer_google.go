package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/api/idtoken"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
)

const providerGoogle = "google"

type BuyerIdentityStore interface {
	UpsertBuyerIdentity(ctx context.Context, in store.BuyerIdentity) (store.BuyerIdentity, error)
	GetBuyerIdentityByProvider(ctx context.Context, provider, subject string) (store.BuyerIdentity, error)
}

// googleIDTokenValidator is swappable for tests.
type googleIDTokenValidator func(ctx context.Context, raw, audience string) (email, name, picture, sub string, emailVerified bool, err error)

func defaultGoogleValidate(ctx context.Context, raw, audience string) (email, name, picture, sub string, emailVerified bool, err error) {
	payload, err := idtoken.Validate(ctx, raw, audience)
	if err != nil {
		return "", "", "", "", false, err
	}
	sub, _ = payload.Claims["sub"].(string)
	email, _ = payload.Claims["email"].(string)
	name, _ = payload.Claims["name"].(string)
	picture, _ = payload.Claims["picture"].(string)
	switch v := payload.Claims["email_verified"].(type) {
	case bool:
		emailVerified = v
	case string:
		emailVerified = strings.EqualFold(v, "true")
	}
	return email, name, picture, sub, emailVerified, nil
}

func googleOAuthClientID() string {
	// Prefer server env; fall back to Vite-named var for single-config local demos.
	for _, k := range []string{"GATEFLOW_GOOGLE_CLIENT_ID", "GOOGLE_OAUTH_CLIENT_ID", "VITE_GOOGLE_CLIENT_ID"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) mountBuyerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/buyer/google", s.buyerGoogleAuth)
	mux.HandleFunc("GET /v1/auth/buyer/providers", s.buyerAuthProviders)
}

func (s *Server) buyerAuthProviders(w http.ResponseWriter, r *http.Request) {
	cid := googleOAuthClientID()
	httpx.WriteJSON(w, 200, map[string]any{
		"providers": []map[string]any{
			{
				"id":         providerGoogle,
				"enabled":    cid != "",
				"client_id":  cid, // public OAuth web client id (not a secret)
				"label":      "Google",
			},
		},
		"local_password": false,
		"note":           "Buyers authenticate only via third-party providers. No local buyer passwords.",
	})
}

func (s *Server) buyerGoogleAuth(w http.ResponseWriter, r *http.Request) {
	corr := httpx.CorrelationID(r)
	clientID := googleOAuthClientID()
	if clientID == "" {
		httpx.WriteError(w, 503, "SALE_AUTH_NOT_CONFIGURED",
			"Google buyer auth is not configured (set GATEFLOW_GOOGLE_CLIENT_ID)", corr, "failed", false)
		return
	}
	var req struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.IDToken) == "" {
		httpx.WriteError(w, 400, "SALE_BAD_REQUEST", "id_token required", corr, "failed", false)
		return
	}

	validate := s.googleValidate
	if validate == nil {
		validate = defaultGoogleValidate
	}
	email, name, picture, sub, verified, err := validate(r.Context(), req.IDToken, clientID)
	if err != nil {
		httpx.WriteError(w, 401, "SALE_GOOGLE_INVALID", "invalid Google id_token: "+err.Error(), corr, "failed", false)
		return
	}
	if sub == "" {
		httpx.WriteError(w, 401, "SALE_GOOGLE_INVALID", "missing Google subject", corr, "failed", false)
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !verified {
		httpx.WriteError(w, 401, "SALE_GOOGLE_EMAIL", "verified email required from Google", corr, "failed", false)
		return
	}

	bis, ok := s.Store.(BuyerIdentityStore)
	if !ok {
		httpx.WriteError(w, 503, "SALE_UNAVAILABLE", "buyer identity store unavailable", corr, "failed", true)
		return
	}

	// Stable Gateflow buyer_id: google_<sub> (unique, opaque, never changes if email changes).
	buyerID := "google_" + sanitizeBuyerFragment(sub)
	if len(buyerID) > 64 {
		buyerID = buyerID[:64]
	}

	// Preserve existing buyer_id if this Google account already linked.
	if existing, gerr := bis.GetBuyerIdentityByProvider(r.Context(), providerGoogle, sub); gerr == nil && existing.BuyerID != "" {
		buyerID = existing.BuyerID
	}

	ident, err := bis.UpsertBuyerIdentity(r.Context(), store.BuyerIdentity{
		ID:              ids.New("bid"),
		Provider:        providerGoogle,
		ProviderSubject: sub,
		Email:           email,
		EmailVerified:   verified,
		DisplayName:     strings.TrimSpace(name),
		PictureURL:      strings.TrimSpace(picture),
		BuyerID:         buyerID,
	})
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}

	secret := os.Getenv("GATEFLOW_DEV_JWT_SECRET")
	tok, err := authn.MintBuyerJWT(secret, ident.BuyerID, []string{authn.ScopeBuyerSelf}, 8*time.Hour)
	if err != nil {
		httpx.WriteError(w, 500, "SALE_INTERNAL", err.Error(), corr, "failed", true)
		return
	}

	httpx.WriteJSON(w, 200, map[string]any{
		"access_token": tok,
		"token_type":   "Bearer",
		"expires_in":   8 * 3600,
		"buyer": map[string]any{
			"buyer_id":     ident.BuyerID,
			"email":        ident.Email,
			"display_name": ident.DisplayName,
			"picture_url":  ident.PictureURL,
			"provider":     providerGoogle,
		},
	})
}

func sanitizeBuyerFragment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return out
}

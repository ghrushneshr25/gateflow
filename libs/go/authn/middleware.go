package authn

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type ctxKey int

const (
	claimsKey ctxKey = 1
	invalidTokenKey ctxKey = 2
)

// ClaimsFromContext returns verified claims if present.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

// Middleware verifies Bearer dev JWT when present and attaches claims.
// Invalid bearer is marked so protected routes fail closed (not treated as anonymous).
func Middleware(next http.Handler) http.Handler {
	signer := NewDevSigner(os.Getenv("GATEFLOW_DEV_JWT_SECRET"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			tok := strings.TrimSpace(h[7:])
			c, err := signer.Verify(tok)
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), claimsKey, c))
			} else {
				r = r.WithContext(context.WithValue(r.Context(), invalidTokenKey, true))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func HasScope(c Claims, want string) bool {
	for _, s := range c.Scopes {
		if s == want || s == ScopeWildcard {
			return true
		}
	}
	return false
}

// IsOperatorOrService reports privileged principal with optional scope.
// Prefer Require* helpers for new code (fail-closed).
func IsOperatorOrService(c Claims, scope string) bool {
	if c.PrincipalType != PrincipalOperator && c.PrincipalType != PrincipalService {
		return false
	}
	if scope == "" {
		return true
	}
	return HasScope(c, scope)
}

// MintServiceJWT creates a service token with explicit scopes only (never injects "*").
func MintServiceJWT(secret, subject string, scopes []string, ttl time.Duration) (string, error) {
	if subject == "" {
		subject = "service"
	}
	clean := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" || s == ScopeWildcard {
			continue
		}
		clean = append(clean, s)
	}
	if len(clean) == 0 {
		return "", ErrForbidden
	}
	return NewDevSigner(secret).Sign(Claims{
		Subject:       subject,
		PrincipalType: PrincipalService,
		Scopes:        clean,
	}, ttl)
}

// MintOperatorJWT creates an operator token with explicit scopes (no implicit "*").
func MintOperatorJWT(secret, subject string, scopes []string, ttl time.Duration) (string, error) {
	if subject == "" {
		subject = "operator"
	}
	clean := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		clean = append(clean, s)
	}
	if len(clean) == 0 {
		return "", ErrForbidden
	}
	return NewDevSigner(secret).Sign(Claims{
		Subject:       subject,
		PrincipalType: PrincipalOperator,
		Scopes:        clean,
	}, ttl)
}

// MintBuyerJWT creates a buyer token. subject is the canonical buyer_id (sub claim).
func MintBuyerJWT(secret, buyerID string, scopes []string, ttl time.Duration) (string, error) {
	buyerID = strings.TrimSpace(buyerID)
	if buyerID == "" {
		return "", errors.New("buyer subject required")
	}
	if len(scopes) == 0 {
		scopes = []string{ScopeBuyerSelf}
	}
	clean := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" || s == ScopeWildcard {
			continue
		}
		clean = append(clean, s)
	}
	if len(clean) == 0 {
		return "", ErrForbidden
	}
	return NewDevSigner(secret).Sign(Claims{
		Subject:       buyerID,
		PrincipalType: PrincipalBuyer,
		Scopes:        clean,
	}, ttl)
}

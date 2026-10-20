package authn

import (
	"context"
	"net/http"
	"os"
	"strings"
)

type ctxKey int

const claimsKey ctxKey = 1

// ClaimsFromContext returns verified claims if present.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

// Middleware verifies Bearer dev JWT when present and attaches claims.
func Middleware(next http.Handler) http.Handler {
	signer := NewDevSigner(os.Getenv("GATEFLOW_DEV_JWT_SECRET"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			tok := strings.TrimSpace(h[7:])
			c, err := signer.Verify(tok)
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), claimsKey, c))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func HasScope(c Claims, want string) bool {
	for _, s := range c.Scopes {
		if s == want || s == "*" {
			return true
		}
	}
	return false
}

// IsOperatorOrService reports privileged principal with optional scope.
func IsOperatorOrService(c Claims, scope string) bool {
	if c.PrincipalType != "operator" && c.PrincipalType != "service" {
		return false
	}
	if scope == "" {
		return true
	}
	return HasScope(c, scope)
}

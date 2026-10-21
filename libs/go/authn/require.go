package authn

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrInvalidToken    = errors.New("invalid token")
	ErrForbidden       = errors.New("forbidden")
)

// Require returns verified claims that match an allowed principal type and at least one scope.
// If scopes is empty, any scope is accepted after principal check.
// Fail-closed: missing auth, invalid bearer, wrong principal, or missing scope → error.
func Require(r *http.Request, principals []string, scopes ...string) (Claims, error) {
	if r.Context().Value(invalidTokenKey) == true {
		return Claims{}, ErrInvalidToken
	}
	c, ok := ClaimsFromContext(r.Context())
	if !ok {
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return Claims{}, ErrInvalidToken
		}
		return Claims{}, ErrUnauthenticated
	}
	if !principalAllowed(c.PrincipalType, principals) {
		return Claims{}, ErrForbidden
	}
	if len(scopes) == 0 {
		return c, nil
	}
	for _, s := range scopes {
		if HasScope(c, s) {
			return c, nil
		}
	}
	return Claims{}, ErrForbidden
}

func principalAllowed(got string, allow []string) bool {
	for _, a := range allow {
		if got == a {
			return true
		}
	}
	return false
}

// RequireOperator is Require for operator with any of the given scopes.
func RequireOperator(r *http.Request, scopes ...string) (Claims, error) {
	return Require(r, []string{PrincipalOperator}, scopes...)
}

// RequireService is Require for service with any of the given scopes.
func RequireService(r *http.Request, scopes ...string) (Claims, error) {
	return Require(r, []string{PrincipalService}, scopes...)
}

// RequireOperatorOrService allows operator or service with any of the scopes.
func RequireOperatorOrService(r *http.Request, scopes ...string) (Claims, error) {
	return Require(r, []string{PrincipalOperator, PrincipalService}, scopes...)
}

// RequireBuyer returns verified buyer claims. Identity is Claims.Subject only.
// Does not read X-Buyer-ID. Invalid/missing bearer fails closed (no header fallback).
func RequireBuyer(r *http.Request, scopes ...string) (Claims, error) {
	if len(scopes) == 0 {
		scopes = []string{ScopeBuyerSelf}
	}
	c, err := Require(r, []string{PrincipalBuyer}, scopes...)
	if err != nil {
		return c, err
	}
	if strings.TrimSpace(c.Subject) == "" {
		return c, ErrUnauthenticated
	}
	return c, nil
}

// BuyerID is the canonical buyer identity from a verified buyer principal.
func BuyerID(c Claims) string {
	return strings.TrimSpace(c.Subject)
}

// StatusCode maps authn errors to HTTP status.
func StatusCode(err error) int {
	if errors.Is(err, ErrForbidden) {
		return http.StatusForbidden
	}
	if errors.Is(err, ErrUnauthenticated) || errors.Is(err, ErrInvalidToken) {
		return http.StatusUnauthorized
	}
	return http.StatusUnauthorized
}

// ValidateProductionSecret refuses known-default or empty secrets outside local/dev/test.
func ValidateProductionSecret(environment, secret string) error {
	env := strings.ToLower(strings.TrimSpace(environment))
	if env == "" || env == "local" || env == "dev" || env == "development" || env == "test" {
		return nil
	}
	// production, staging, prod, etc.
	if secret == "" || secret == DefaultDevSecret {
		return errors.New("GATEFLOW_DEV_JWT_SECRET must be set to a non-default value when ENVIRONMENT is " + environment)
	}
	if len(secret) < 16 {
		return errors.New("GATEFLOW_DEV_JWT_SECRET must be at least 16 characters in non-local environments")
	}
	return nil
}

// MustValidateProductionSecret reads ENVIRONMENT and GATEFLOW_DEV_JWT_SECRET from the environment.
func MustValidateProductionSecret() error {
	return ValidateProductionSecret(os.Getenv("ENVIRONMENT"), os.Getenv("GATEFLOW_DEV_JWT_SECRET"))
}



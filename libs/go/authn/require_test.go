package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateProductionSecret(t *testing.T) {
	if err := ValidateProductionSecret("local", DefaultDevSecret); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProductionSecret("production", DefaultDevSecret); err == nil {
		t.Fatal("expected error")
	}
}

func TestMintServiceJWTRejectsWildcardOnly(t *testing.T) {
	_, err := MintServiceJWT(DefaultDevSecret, "order", []string{"*"}, time.Hour)
	if err == nil {
		t.Fatal("expected reject")
	}
	tok, err := MintServiceJWT(DefaultDevSecret, "order", OrderServiceScopes, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewDevSigner(DefaultDevSecret).Verify(tok)
	if err != nil || HasScope(c, ScopeWildcard) || !HasScope(c, ScopeInventoryWrite) {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestRequireFailClosed(t *testing.T) {
	good, _ := MintOperatorJWT(DefaultDevSecret, "op", []string{ScopeSaleWrite}, time.Hour)
	svc, _ := MintServiceJWT(DefaultDevSecret, "inv", []string{ScopeInventoryWrite}, time.Hour)
	run := func(auth string, princ []string, scopes []string) error {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		var got error
		Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, got = Require(r, princ, scopes...)
		})).ServeHTTP(httptest.NewRecorder(), req)
		return got
	}
	if err := run("", []string{PrincipalOperator}, []string{ScopeSaleWrite}); err != ErrUnauthenticated {
		t.Fatal(err)
	}
	if err := run("Bearer bad", []string{PrincipalOperator}, []string{ScopeSaleWrite}); err != ErrInvalidToken {
		t.Fatal(err)
	}
	if err := run("Bearer "+svc, []string{PrincipalOperator}, []string{ScopeSaleWrite}); err != ErrForbidden {
		t.Fatal(err)
	}
	if err := run("Bearer "+good, []string{PrincipalOperator}, []string{ScopeSaleWrite}); err != nil {
		t.Fatal(err)
	}
}

func TestRequireBuyerNoHeaderFallback(t *testing.T) {
	buyerTok, err := MintBuyerJWT(DefaultDevSecret, "buyer_a", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+buyerTok)
	req.Header.Set("X-Buyer-ID", "buyer_b")
	var id string
	Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := RequireBuyer(r)
		if e != nil {
			t.Fatalf("%v", e)
		}
		id = BuyerID(c)
	})).ServeHTTP(httptest.NewRecorder(), req)
	if id != "buyer_a" {
		t.Fatalf("got %s", id)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-valid")
	req.Header.Set("X-Buyer-ID", "buyer_a")
	var got error
	Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, got = RequireBuyer(r)
	})).ServeHTTP(httptest.NewRecorder(), req)
	if got != ErrInvalidToken {
		t.Fatalf("got %v", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Buyer-ID", "buyer_a")
	Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, got = RequireBuyer(r)
	})).ServeHTTP(httptest.NewRecorder(), req)
	if got != ErrUnauthenticated {
		t.Fatalf("got %v", got)
	}
}

func TestContextClaims(t *testing.T) {
	c := Claims{Subject: "x", PrincipalType: PrincipalService}
	ctx := context.WithValue(context.Background(), claimsKey, c)
	got, ok := ClaimsFromContext(ctx)
	if !ok || got.Subject != "x" {
		t.Fatal(got)
	}
}

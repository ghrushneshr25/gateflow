package authn

import (
	"testing"
	"time"
)

func TestDevTokenRoundTrip(t *testing.T) {
	s := NewDevSigner("test-secret")
	tok, err := s.Sign(Claims{
		Subject:       "buyer_1",
		PrincipalType: "buyer",
		Scopes:        []string{"buyer:checkout"},
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "buyer_1" || c.PrincipalType != "buyer" {
		t.Fatalf("%+v", c)
	}
}

func TestDevTokenRejectsTamper(t *testing.T) {
	s := NewDevSigner("test-secret")
	tok, err := s.Sign(Claims{Subject: "x", PrincipalType: "buyer"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Verify(tok + "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

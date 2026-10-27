package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
)

type memBuyers struct {
	byKey map[string]store.BuyerIdentity
}

func (m *memBuyers) UpsertBuyerIdentity(ctx context.Context, in store.BuyerIdentity) (store.BuyerIdentity, error) {
	if m.byKey == nil {
		m.byKey = map[string]store.BuyerIdentity{}
	}
	k := in.Provider + "|" + in.ProviderSubject
	if old, ok := m.byKey[k]; ok {
		in.ID = old.ID
		in.BuyerID = old.BuyerID
		in.CreatedAt = old.CreatedAt
	}
	m.byKey[k] = in
	return in, nil
}

func (m *memBuyers) GetBuyerIdentityByProvider(ctx context.Context, provider, subject string) (store.BuyerIdentity, error) {
	if m.byKey == nil {
		return store.BuyerIdentity{}, store.ErrNotFound
	}
	u, ok := m.byKey[provider+"|"+subject]
	if !ok {
		return store.BuyerIdentity{}, store.ErrNotFound
	}
	return u, nil
}

type fakeAllStore struct {
	*memUsers
	*memBuyers
}

func (f *fakeAllStore) CreateOffering(ctx context.Context, o store.Offering) error { return nil }
func (f *fakeAllStore) GetOffering(ctx context.Context, id string) (store.Offering, error) {
	return store.Offering{}, store.ErrNotFound
}
func (f *fakeAllStore) ListOfferings(ctx context.Context, publishedOnly bool) ([]store.Offering, error) {
	return nil, nil
}
func (f *fakeAllStore) CreateSession(ctx context.Context, sess store.Session, policy store.PurchasePolicy) error {
	return nil
}
func (f *fakeAllStore) ListSessions(ctx context.Context, offeringID string) ([]store.Session, error) {
	return nil, nil
}
func (f *fakeAllStore) GetSession(ctx context.Context, id string) (store.Session, error) {
	return store.Session{}, store.ErrNotFound
}
func (f *fakeAllStore) CreateTicketType(ctx context.Context, tt store.TicketType) error { return nil }
func (f *fakeAllStore) ListTicketTypes(ctx context.Context, sessionID string) ([]store.TicketType, error) {
	return nil, nil
}
func (f *fakeAllStore) SetTicketPoolID(ctx context.Context, ticketTypeID, poolID string) error {
	return nil
}
func (f *fakeAllStore) LatestPolicy(ctx context.Context, sessionID string) (store.PurchasePolicy, error) {
	return store.PurchasePolicy{}, store.ErrNotFound
}
func (f *fakeAllStore) PublishOffering(ctx context.Context, offeringID string, now time.Time) (store.Offering, []store.Session, []store.TicketType, error) {
	return store.Offering{}, nil, nil, nil
}

func TestBuyerGoogleAuth_IssuesJWTAndPersistsIdentity(t *testing.T) {
	_ = os.Setenv("GATEFLOW_DEV_JWT_SECRET", authn.DefaultDevSecret)
	_ = os.Setenv("GATEFLOW_GOOGLE_CLIENT_ID", "test-client.apps.googleusercontent.com")
	t.Cleanup(func() { _ = os.Unsetenv("GATEFLOW_GOOGLE_CLIENT_ID") })

	fs := &fakeAllStore{memUsers: &memUsers{}, memBuyers: &memBuyers{}}
	s := NewWithDeps(fs, nil, "")
	s.googleValidate = func(ctx context.Context, raw, audience string) (email, name, picture, sub string, emailVerified bool, err error) {
		if audience != "test-client.apps.googleusercontent.com" {
			t.Fatalf("audience %s", audience)
		}
		if raw != "fake-id-token" {
			t.Fatalf("token %s", raw)
		}
		return "alex@gmail.com", "Alex", "https://example.com/p.png", "google-sub-99", true, nil
	}
	mux := http.NewServeMux()
	s.Routes(mux)
	h := authn.Middleware(mux)

	body, _ := json.Marshal(map[string]string{"id_token": "fake-id-token"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/buyer/google", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	tok, _ := out["access_token"].(string)
	if tok == "" {
		t.Fatal("no access_token")
	}
	c, err := authn.NewDevSigner(authn.DefaultDevSecret).Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.PrincipalType != authn.PrincipalBuyer || c.Subject == "" {
		t.Fatalf("claims %+v", c)
	}
	buyer, _ := out["buyer"].(map[string]any)
	if buyer["email"] != "alex@gmail.com" {
		t.Fatalf("buyer %+v", buyer)
	}
	// second login same google sub keeps buyer_id
	body2, _ := json.Marshal(map[string]string{"id_token": "fake-id-token"})
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/v1/auth/buyer/google", bytes.NewReader(body2)))
	var out2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &out2)
	b2, _ := out2["buyer"].(map[string]any)
	if b2["buyer_id"] != buyer["buyer_id"] {
		t.Fatalf("buyer_id changed %v vs %v", buyer["buyer_id"], b2["buyer_id"])
	}
}

func TestBuyerProviders(t *testing.T) {
	_ = os.Setenv("GATEFLOW_GOOGLE_CLIENT_ID", "cid.apps.googleusercontent.com")
	t.Cleanup(func() { _ = os.Unsetenv("GATEFLOW_GOOGLE_CLIENT_ID") })
	s := NewWithDeps(&fakeAllStore{memUsers: &memUsers{}, memBuyers: &memBuyers{}}, nil, "")
	mux := http.NewServeMux()
	s.Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/buyer/providers", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["local_password"] != false {
		t.Fatalf("%v", out)
	}
}

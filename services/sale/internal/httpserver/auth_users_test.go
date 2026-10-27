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
	"golang.org/x/crypto/bcrypt"
)

type memUsers struct {
	byUser map[string]store.OperatorUser
}

func (m *memUsers) CreateOperatorUser(ctx context.Context, u store.OperatorUser) error {
	if m.byUser == nil {
		m.byUser = map[string]store.OperatorUser{}
	}
	k := u.Username
	if _, ok := m.byUser[k]; ok {
		return store.ErrConflict
	}
	m.byUser[k] = u
	return nil
}

func (m *memUsers) GetOperatorUserByUsername(ctx context.Context, username string) (store.OperatorUser, error) {
	u, ok := m.byUser[username]
	if !ok {
		// try lower
		for k, v := range m.byUser {
			if k == username || equalFold(k, username) {
				return v, nil
			}
		}
		return store.OperatorUser{}, store.ErrNotFound
	}
	return u, nil
}

func equalFold(a, b string) bool {
	return len(a) == len(b) && (a == b || stringEqualFold(a, b))
}

func stringEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func (m *memUsers) ListOperatorUsers(ctx context.Context) ([]store.OperatorUser, error) {
	var out []store.OperatorUser
	for _, u := range m.byUser {
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, nil
}

func (m *memUsers) CountOperatorUsers(ctx context.Context) (int, error) {
	return len(m.byUser), nil
}

// embed mem into a fake SaleStore for Server
type fakeStore struct {
	*memUsers
}

func (f *fakeStore) CreateOffering(ctx context.Context, o store.Offering) error { return nil }
func (f *fakeStore) GetOffering(ctx context.Context, id string) (store.Offering, error) {
	return store.Offering{}, store.ErrNotFound
}
func (f *fakeStore) ListOfferings(ctx context.Context, publishedOnly bool) ([]store.Offering, error) {
	return nil, nil
}
func (f *fakeStore) CreateSession(ctx context.Context, sess store.Session, policy store.PurchasePolicy) error {
	return nil
}
func (f *fakeStore) ListSessions(ctx context.Context, offeringID string) ([]store.Session, error) {
	return nil, nil
}
func (f *fakeStore) GetSession(ctx context.Context, id string) (store.Session, error) {
	return store.Session{}, store.ErrNotFound
}
func (f *fakeStore) CreateTicketType(ctx context.Context, tt store.TicketType) error { return nil }
func (f *fakeStore) ListTicketTypes(ctx context.Context, sessionID string) ([]store.TicketType, error) {
	return nil, nil
}
func (f *fakeStore) SetTicketPoolID(ctx context.Context, ticketTypeID, poolID string) error {
	return nil
}
func (f *fakeStore) LatestPolicy(ctx context.Context, sessionID string) (store.PurchasePolicy, error) {
	return store.PurchasePolicy{}, store.ErrNotFound
}
func (f *fakeStore) PublishOffering(ctx context.Context, offeringID string, now time.Time) (store.Offering, []store.Session, []store.TicketType, error) {
	return store.Offering{}, nil, nil, nil
}

func TestBootstrapAndLogin(t *testing.T) {
	_ = os.Setenv("GATEFLOW_DEV_JWT_SECRET", authn.DefaultDevSecret)
	_ = os.Setenv("GATEFLOW_ADMIN_USERNAME", "admin")
	_ = os.Setenv("GATEFLOW_ADMIN_PASSWORD", "GateflowAdmin1!")
	t.Cleanup(func() {
		_ = os.Unsetenv("GATEFLOW_ADMIN_USERNAME")
		_ = os.Unsetenv("GATEFLOW_ADMIN_PASSWORD")
	})

	mem := &memUsers{}
	created, user, err := BootstrapDefaultAdmin(context.Background(), mem)
	if err != nil || !created || user != "admin" {
		t.Fatalf("bootstrap created=%v user=%s err=%v", created, user, err)
	}
	// second call no-op
	created2, _, err := BootstrapDefaultAdmin(context.Background(), mem)
	if err != nil || created2 {
		t.Fatalf("second bootstrap created=%v err=%v", created2, err)
	}

	fs := &fakeStore{memUsers: mem}
	s := NewWithDeps(fs, nil, "http://127.0.0.1:8083")
	mux := http.NewServeMux()
	s.Routes(mux)
	h := authn.Middleware(mux)

	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "GateflowAdmin1!"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login status %d body %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	tok, _ := out["access_token"].(string)
	if tok == "" {
		t.Fatal("missing token")
	}
	body2, _ := json.Marshal(map[string]string{"username": "alice", "password": "OtherPass1!"})
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/users", bytes.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer "+tok)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 201 {
		t.Fatalf("create user %d %s", rec2.Code, rec2.Body.String())
	}
}

func TestLoginBadPassword(t *testing.T) {
	_ = os.Setenv("GATEFLOW_DEV_JWT_SECRET", authn.DefaultDevSecret)
	mem := &memUsers{}
	hash, _ := bcrypt.GenerateFromPassword([]byte("GateflowAdmin1!"), bcrypt.DefaultCost)
	_ = mem.CreateOperatorUser(context.Background(), store.OperatorUser{
		ID: "ops_1", Username: "admin", PasswordHash: string(hash),
		Scopes: defaultOperatorScopes, Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	s := NewWithDeps(&fakeStore{memUsers: mem}, nil, "")
	mux := http.NewServeMux()
	s.Routes(mux)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401 got %d", rec.Code)
	}
}

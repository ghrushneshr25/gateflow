package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
)

func TestHealthz(t *testing.T) {
	mux := http.NewServeMux()
	MountHealth(mux, "test", nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	CorrelationMiddleware(mux).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if rr.Header().Get(HeaderCorrelationID) == "" {
		t.Fatal("missing correlation id")
	}
	var h Health
	if err := json.NewDecoder(rr.Body).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.Service != "test" || h.Status != "ok" {
		t.Fatalf("unexpected body: %+v", h)
	}
}

func TestReadyzFailure(t *testing.T) {
	mux := http.NewServeMux()
	MountHealth(mux, "test", func() error { return errReady })
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestCorrelationValidation(t *testing.T) {
	if ValidCorrelationID("not a id!!!") {
		t.Fatal("reject garbage")
	}
	if ValidCorrelationID("") {
		t.Fatal("reject empty")
	}
	id := ids.ULID()
	if !ValidCorrelationID(id) {
		t.Fatalf("accept ulid %s", id)
	}
	// inject attempt
	if ValidCorrelationID("01ARZ3NDEKTSV4RRFFQ69G5FAV extra") {
		t.Fatal("reject injection")
	}
}

func TestCorrelationMiddlewareReplacesInvalid(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {
		id := CorrelationID(r)
		if !ValidCorrelationID(id) {
			t.Errorf("bad id %q", id)
		}
		if id == "BAD!!!" {
			t.Error("must not trust invalid")
		}
		w.WriteHeader(204)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(HeaderCorrelationID, "BAD!!!")
	rr := httptest.NewRecorder()
	CorrelationMiddleware(mux).ServeHTTP(rr, req)
	got := rr.Header().Get(HeaderCorrelationID)
	if !ValidCorrelationID(got) || got == "BAD!!!" {
		t.Fatalf("got %q", got)
	}
}

func TestCorrelationMiddlewarePropagatesValid(t *testing.T) {
	id := ids.ULID()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {
		if CorrelationID(r) != id {
			t.Errorf("want %s got %s", id, CorrelationID(r))
		}
		cid, ok := CorrelationFromContext(r.Context())
		if !ok || cid != id {
			t.Errorf("ctx %v %v", cid, ok)
		}
		w.WriteHeader(204)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(HeaderCorrelationID, id)
	rr := httptest.NewRecorder()
	CorrelationMiddleware(mux).ServeHTTP(rr, req)
	if rr.Header().Get(HeaderCorrelationID) != id {
		t.Fatal(rr.Header().Get(HeaderCorrelationID))
	}
}

func TestLoggerAddsCorrelation(t *testing.T) {
	ctx := WithCorrelation(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	_ = Logger(ctx, nil) // smoke
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const errReady simpleError = "db down"

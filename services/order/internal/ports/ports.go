package ports

import (
	"context"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// OrderStore is the persistence port used by the HTTP layer (mockable via nexus).
type OrderStore interface {
	GetByIdempotency(ctx context.Context, buyerID, key string) (store.Order, error)
	Get(ctx context.Context, id string) (store.Order, error)
	ListByBuyer(ctx context.Context, buyerID string) ([]store.Order, error)
	CreateWithHold(ctx context.Context, o store.Order, holdID string, now time.Time) (store.Order, error)
	Update(ctx context.Context, o store.Order) error
	Confirm(ctx context.Context, orderID, eventID, corr string, payload any, now time.Time) (store.Order, error)
	Fail(ctx context.Context, orderID string, now time.Time) error
	HeldAndCommittedQty(ctx context.Context, buyerID, sessionID string) (int, error)
	MarkEventProcessed(ctx context.Context, eventID string) (bool, error)
}

// HTTPDoer abstracts net/http.Client for outbound S2S calls.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// APIServer mounts order routes.
type APIServer interface {
	Routes(mux *http.ServeMux)
}

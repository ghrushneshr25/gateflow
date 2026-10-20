package ports

import (
	"context"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/inventory/internal/store"
)

type InventoryStore interface {
	UpsertPool(ctx context.Context, p store.Pool) (store.Pool, error)
	GetPool(ctx context.Context, id string) (store.Pool, error)
	Reserve(ctx context.Context, res store.Reservation) (store.Reservation, error)
	GetByOperation(ctx context.Context, operationID string) (store.Reservation, error)
	GetReservation(ctx context.Context, id string) (store.Reservation, error)
	Confirm(ctx context.Context, id string, now time.Time) (store.Reservation, error)
	Release(ctx context.Context, id string, now time.Time) (store.Reservation, error)
	Expire(ctx context.Context, id string, now time.Time) (store.Reservation, error)
	ExpireDue(ctx context.Context, now time.Time, limit int) (int, error)
}

type APIServer interface {
	Routes(mux *http.ServeMux)
}

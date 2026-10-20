package ports

import (
	"context"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/payment-simulator/internal/store"
)

type PaymentStore interface {
	CreateOrGet(ctx context.Context, p store.Payment) (store.Payment, error)
	UpdateStatus(ctx context.Context, id, status string, now time.Time) (store.Payment, error)
	GetByOperation(ctx context.Context, op string) (store.Payment, error)
}

type APIServer interface {
	Routes(mux *http.ServeMux)
}

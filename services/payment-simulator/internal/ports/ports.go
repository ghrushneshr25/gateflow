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
	ApplyCallback(ctx context.Context, operationID, status string, now time.Time) (store.Payment, error)
	ResolveDue(ctx context.Context, now time.Time, limit int) (int, error)
	CreateOrGetCompensation(ctx context.Context, c store.Compensation) (store.Compensation, error)
	GetCompensationByOperation(ctx context.Context, op string) (store.Compensation, error)
	GetCompensation(ctx context.Context, id string) (store.Compensation, error)
	ExecuteCompensationAttempt(ctx context.Context, operationID string, forceFail bool, now time.Time) (store.Compensation, error)
	MarkCompensationUnknown(ctx context.Context, operationID string, now time.Time) (store.Compensation, error)
}

type APIServer interface {
	Routes(mux *http.ServeMux)
}

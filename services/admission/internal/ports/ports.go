package ports

import (
	"context"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

type AdmissionStore interface {
	Join(ctx context.Context, entry store.QueueEntry) (store.QueueEntry, error)
	GetMine(ctx context.Context, sessionID, buyerID string) (store.QueueEntry, error)
	GrantFIFO(ctx context.Context, sessionID, buyerID, entitlementID string, unboundTTL time.Duration, now time.Time) (store.QueueEntry, store.Entitlement, error)
	GetEntitlement(ctx context.Context, id string) (store.Entitlement, error)
	Bind(ctx context.Context, id, buyerID, checkoutOp string, now time.Time) (store.Entitlement, error)
	Consume(ctx context.Context, id, checkoutOp string, now time.Time) (store.Entitlement, error)
	EnsureBudgets(ctx context.Context, sessionID string, maxGrants, maxActive int) error
	GetBudget(ctx context.Context, scopeKey string) (store.Budget, error)
	SetBudgetCaps(ctx context.Context, scopeKey string, maxGrants, maxActive int) error
	MarkEventProcessed(ctx context.Context, eventID string) (bool, error)
	GetCoordination(ctx context.Context) (store.Coordination, error)
	FenceAdmission(ctx context.Context, reason, actor string, now time.Time) (store.Coordination, error)
	UnfenceAdmission(ctx context.Context, actor string, now time.Time) (store.Coordination, error)
}

type APIServer interface {
	Routes(mux *http.ServeMux)
}

package ports

import (
	"context"
	"net/http"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
)

type SaleStore interface {
	CreateOffering(ctx context.Context, o store.Offering) error
	GetOffering(ctx context.Context, id string) (store.Offering, error)
	ListOfferings(ctx context.Context, publishedOnly bool) ([]store.Offering, error)
	CreateSession(ctx context.Context, sess store.Session, policy store.PurchasePolicy) error
	ListSessions(ctx context.Context, offeringID string) ([]store.Session, error)
	GetSession(ctx context.Context, id string) (store.Session, error)
	CreateTicketType(ctx context.Context, tt store.TicketType) error
	ListTicketTypes(ctx context.Context, sessionID string) ([]store.TicketType, error)
	SetTicketPoolID(ctx context.Context, ticketTypeID, poolID string) error
	LatestPolicy(ctx context.Context, sessionID string) (store.PurchasePolicy, error)
	PublishOffering(ctx context.Context, offeringID string, now time.Time) (store.Offering, []store.Session, []store.TicketType, error)
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type APIServer interface {
	Routes(mux *http.ServeMux)
}

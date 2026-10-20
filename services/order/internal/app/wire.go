package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ghrushneshr25/nexus"
	"github.com/ghrushneshrathod/gateflow/libs/go/config"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/ports"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeConfig holds process wiring inputs declared as a nexus value.
type RuntimeConfig struct {
	Service          config.Service
	DatabaseURL      string
	SearchPath       string
	SaleBaseURL      string
	AdmissionBaseURL string
	InventoryBaseURL string
	PaymentBaseURL   string
	HTTPTimeout      time.Duration
}

func OrderStoreContract() nexus.Contract[ports.OrderStore] {
	return nexus.ContractOf[ports.OrderStore]()
}

func HTTPDoerContract() nexus.Contract[ports.HTTPDoer] {
	return nexus.ContractOf[ports.HTTPDoer]()
}

func APIServerContract() nexus.Contract[ports.APIServer] {
	return nexus.ContractOf[ports.APIServer]()
}

// Wire registers production constructors and values. Safe to call once per process.
// Tests should nexus.Reset() then register mocks instead of calling Wire.
func Wire(rt RuntimeConfig) (*pgxpool.Pool, error) {
	if rt.SearchPath == "" {
		rt.SearchPath = "order"
	}
	if rt.HTTPTimeout <= 0 {
		rt.HTTPTimeout = 5 * time.Second
	}
	if rt.SaleBaseURL == "" {
		rt.SaleBaseURL = env("SALE_BASE_URL", "http://127.0.0.1:8081")
	}
	if rt.AdmissionBaseURL == "" {
		rt.AdmissionBaseURL = env("ADMISSION_BASE_URL", "http://127.0.0.1:8082")
	}
	if rt.InventoryBaseURL == "" {
		rt.InventoryBaseURL = env("INVENTORY_BASE_URL", "http://127.0.0.1:8083")
	}
	if rt.PaymentBaseURL == "" {
		rt.PaymentBaseURL = env("PAYMENT_BASE_URL", "http://127.0.0.1:8085")
	}

	nexus.MustDeclareValue(rt)

	pool, err := postgres.NewPoolWithSearchPath(context.Background(), rt.DatabaseURL, rt.SearchPath)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	nexus.MustDeclareValue(pool)

	nexus.MustDeclare(newOrderStore)
	nexus.MustDeclare(newHTTPDoer)
	nexus.MustDeclare(newAPIServer)

	if err := nexus.Validate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("nexus validate: %w", err)
	}
	return pool, nil
}

func newOrderStore(pool *pgxpool.Pool) ports.OrderStore {
	return store.New(pool)
}

func newHTTPDoer(rt RuntimeConfig) ports.HTTPDoer {
	return &http.Client{Timeout: rt.HTTPTimeout}
}

func newAPIServer(st ports.OrderStore, httpDoer ports.HTTPDoer, rt RuntimeConfig) ports.APIServer {
	return httpserver.NewWithDeps(st, httpDoer, httpserver.Deps{
		SaleBase:      rt.SaleBaseURL,
		AdmissionBase: rt.AdmissionBaseURL,
		InventoryBase: rt.InventoryBaseURL,
		PaymentBase:   rt.PaymentBaseURL,
	})
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

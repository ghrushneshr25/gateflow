package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ghrushneshr25/nexus"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/ports"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RuntimeConfig struct {
	DatabaseURL      string
	SearchPath       string
	InventoryBaseURL string
}

func SaleStoreContract() nexus.Contract[ports.SaleStore] {
	return nexus.ContractOf[ports.SaleStore]()
}

func APIServerContract() nexus.Contract[ports.APIServer] {
	return nexus.ContractOf[ports.APIServer]()
}

func Wire(rt RuntimeConfig) (*pgxpool.Pool, error) {
	if rt.SearchPath == "" {
		rt.SearchPath = "sale"
	}
	if rt.InventoryBaseURL == "" {
		rt.InventoryBaseURL = env("INVENTORY_BASE_URL", "http://127.0.0.1:8083")
	}
	nexus.MustDeclareValue(rt)
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), rt.DatabaseURL, rt.SearchPath)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	nexus.MustDeclareValue(pool)
	nexus.MustDeclare(func(p *pgxpool.Pool) ports.SaleStore { return store.New(p) })
	nexus.MustDeclare(func() ports.HTTPDoer { return &http.Client{Timeout: 5 * time.Second} })
	nexus.MustDeclare(func(st ports.SaleStore, h ports.HTTPDoer, rt RuntimeConfig) ports.APIServer {
		return httpserver.NewWithDeps(st, h, rt.InventoryBaseURL)
	})
	if err := nexus.Validate(); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

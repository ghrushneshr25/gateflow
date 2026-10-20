package app

import (
	"context"
	"fmt"

	"github.com/ghrushneshr25/nexus"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/ports"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RuntimeConfig struct {
	DatabaseURL string
	SearchPath  string
}

func AdmissionStoreContract() nexus.Contract[ports.AdmissionStore] {
	return nexus.ContractOf[ports.AdmissionStore]()
}

func APIServerContract() nexus.Contract[ports.APIServer] {
	return nexus.ContractOf[ports.APIServer]()
}

func Wire(rt RuntimeConfig) (*pgxpool.Pool, error) {
	if rt.SearchPath == "" {
		rt.SearchPath = "admission"
	}
	nexus.MustDeclareValue(rt)
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), rt.DatabaseURL, rt.SearchPath)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	nexus.MustDeclareValue(pool)
	nexus.MustDeclare(func(p *pgxpool.Pool) ports.AdmissionStore { return store.New(p) })
	nexus.MustDeclare(func(st ports.AdmissionStore) ports.APIServer { return httpserver.New(st) })
	if err := nexus.Validate(); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

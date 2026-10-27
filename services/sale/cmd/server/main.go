package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ghrushneshr25/nexus"
	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/config"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/logging"
	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/app"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/httpserver"
	"github.com/ghrushneshrathod/gateflow/services/sale/internal/store"
)

func main() {
	cfg := config.LoadService("sale", ":8081")
	log := logging.New(cfg.Name, cfg.LogLevel)
	defer telemetry.Init(log, cfg.Name)()
	if err := authn.MustValidateProductionSecret(); err != nil {
		log.Error("auth config", "err", err)
		os.Exit(1)
	}

	var readyErr error
	var poolClose func()
	dbURL := firstNonEmpty(os.Getenv("SALE_DATABASE_URL"), cfg.DatabaseURL)
	if dbURL == "" {
		readyErr = errNoDB
	} else {
		pool, err := app.Wire(app.RuntimeConfig{DatabaseURL: dbURL, SearchPath: "sale"})
		if err != nil {
			readyErr = err
			log.Error("wire", "err", err)
		} else {
			poolClose = pool.Close
			st := store.New(pool)
			if created, user, berr := httpserver.BootstrapDefaultAdmin(context.Background(), st); berr != nil {
				log.Warn("admin bootstrap", "err", berr)
			} else if created {
				log.Info("default admin user created", "username", user, "hint", "GATEFLOW_ADMIN_PASSWORD")
			}
		}
	}
	if poolClose != nil {
		defer poolClose()
	}

	mux := http.NewServeMux()
	httpx.MountHealth(mux, cfg.Name, func() error { return readyErr })
	mux.HandleFunc("GET /v1/meta", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"service": cfg.Name, "phase": 3, "di": "nexus", "auth": "operator_users"})
	})
	if readyErr == nil {
		nexus.MustGet(app.APIServerContract).Routes(mux)
	}
	srv := httpx.NewServer(cfg.HTTPAddr, httpx.WrapHandler(cfg.Name, authn.Middleware(mux)))
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			os.Exit(1)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sh)
	slog.Info("stopped")
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const errNoDB simpleError = "DATABASE_URL not set"

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

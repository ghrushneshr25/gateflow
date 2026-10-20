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
	"github.com/ghrushneshrathod/gateflow/libs/go/config"
	"github.com/ghrushneshrathod/gateflow/libs/go/httpx"
	"github.com/ghrushneshrathod/gateflow/libs/go/logging"
	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"github.com/ghrushneshrathod/gateflow/services/inventory/internal/app"
)

func main() {
	cfg := config.LoadService("inventory", ":8083")
	log := logging.New(cfg.Name, cfg.LogLevel)
	defer telemetry.Init(log, cfg.Name)()

	var readyErr error
	dbURL := firstNonEmpty(os.Getenv("INVENTORY_DATABASE_URL"), cfg.DatabaseURL)
	if dbURL == "" {
		readyErr = errNoDB
	} else {
		pool, err := app.Wire(app.RuntimeConfig{DatabaseURL: dbURL, SearchPath: "inventory"})
		if err != nil {
			readyErr = err
			log.Error("wire", "err", err)
		} else {
			defer pool.Close()
		}
	}

	mux := http.NewServeMux()
	httpx.MountHealth(mux, cfg.Name, func() error { return readyErr })
	mux.HandleFunc("GET /v1/meta", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 200, map[string]any{"service": cfg.Name, "phase": 3, "di": "nexus"})
	})
	if readyErr == nil {
		nexus.MustGet(app.APIServerContract).Routes(mux)
	}
	srv := httpx.NewServer(cfg.HTTPAddr, httpx.CorrelationMiddleware(mux))
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

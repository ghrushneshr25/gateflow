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
	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/app"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/grantprotocol"
)

func main() {
	cfg := config.LoadService("admission", ":8082")
	log := logging.New(cfg.Name, cfg.LogLevel)
	defer telemetry.Init(log, cfg.Name)()
	if err := authn.MustValidateProductionSecret(); err != nil {
		log.Error("auth config", "err", err)
		os.Exit(1)
	}

	grantProto := grantprotocol.FromEnv()
	rawProto := os.Getenv(grantprotocol.EnvVar)
	if rawProto != "" && !grantprotocol.IsValid(rawProto) {
		log.Warn("invalid grant protocol; defaulting to A", "env", grantprotocol.EnvVar, "value", rawProto)
	}
	metrics.SetAdmissionGrantProtocol(grantProto)
	log.Info("admission grant protocol selected", "protocol", grantProto, "env", grantprotocol.EnvVar)

	var readyErr error
	dbURL := firstNonEmpty(os.Getenv("ADMISSION_DATABASE_URL"), cfg.DatabaseURL)
	if dbURL == "" {
		readyErr = errNoDB
	} else {
		pool, err := app.Wire(app.RuntimeConfig{
			DatabaseURL: dbURL, SearchPath: "admission", GrantProtocol: grantProto,
		})
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
		httpx.WriteJSON(w, 200, map[string]any{
			"service": cfg.Name, "phase": 6, "di": "nexus", "coordination": "pg-fence",
			"grant_protocol": grantProto,
		})
	})
	if readyErr == nil {
		nexus.MustGet(app.APIServerContract).Routes(mux)
	}
	handler := httpx.WrapHandler(cfg.Name, authn.Middleware(mux))
	srv := httpx.NewServer(cfg.HTTPAddr, handler)
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

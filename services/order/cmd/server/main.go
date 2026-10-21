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
	"github.com/ghrushneshrathod/gateflow/libs/go/kafkax"
	"github.com/ghrushneshrathod/gateflow/libs/go/logging"
	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/app"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/outbox"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func main() {
	cfg := config.LoadService("order", ":8084")
	log := logging.New(cfg.Name, cfg.LogLevel)
	defer telemetry.Init(log, cfg.Name)()
	if err := authn.MustValidateProductionSecret(); err != nil {
		log.Error("auth config", "err", err)
		os.Exit(1)
	}

	var readyErr error
	var st *store.Store
	dbURL := firstNonEmpty(os.Getenv("ORDER_DATABASE_URL"), cfg.DatabaseURL)
	if dbURL == "" {
		readyErr = errNoDB
	} else {
		pool, err := app.Wire(app.RuntimeConfig{
			Service:     cfg,
			DatabaseURL: dbURL,
			SearchPath:  "order",
		})
		if err != nil {
			readyErr = err
			log.Error("wire", "err", err)
		} else {
			defer pool.Close()
			st = store.New(pool)
		}
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	var rt *outbox.Runtime
	brokers := kafkax.ParseBrokers(os.Getenv("KAFKA_BROKERS"))
	kafkaEnabled := os.Getenv("KAFKA_ENABLED") != "0" && os.Getenv("KAFKA_ENABLED") != "false"
	if st != nil && kafkaEnabled {
		rt = &outbox.Runtime{
			Store: st, Brokers: brokers, Log: log, Metrics: &outbox.Metrics{},
		}
		go rt.Run(bgCtx)
		log.Info("kafka outbox runtime supervising reconnect", "brokers", brokers)
	} else {
		log.Info("kafka outbox disabled or no store")
	}
	// Low-frequency ops gauges (avoid tight DB loops).
	if st != nil {
		go func() {
			t := time.NewTicker(15 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-bgCtx.Done():
					return
				case <-t.C:
					refreshOrderGauges(bgCtx, st)
				}
			}
		}()
	}

	mux := http.NewServeMux()
	httpx.MountHealth(mux, cfg.Name, func() error { return readyErr })
	mux.HandleFunc("GET /v1/meta", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 200, map[string]any{
			"service": cfg.Name, "phase": 4, "di": "nexus",
			"kafka_enabled": kafkaEnabled && st != nil,
			"kafka_ready":   rt != nil && rt.Ready(),
		})
	})
	mux.HandleFunc("GET /v1/admin/outbox/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !requireOutboxRead(w, r) {
			return
		}
		out := map[string]any{
			"kafka_enabled": kafkaEnabled && st != nil,
			"kafka_ready":   rt != nil && rt.Ready(),
		}
		if st != nil {
			age, n, err := st.OutboxBacklog(r.Context())
			if err == nil {
				out["unpublished_count"] = n
				out["oldest_age_ms"] = age.Milliseconds()
			}
			q, _ := st.CountQuarantined(r.Context())
			out["quarantine_count"] = q
		}
		if rt != nil && rt.Metrics != nil {
			out["publish_ok"] = rt.Metrics.PublishOK.Load()
			out["publish_fail"] = rt.Metrics.PublishFail.Load()
			out["claimed"] = rt.Metrics.Claimed.Load()
		}
		if rt != nil && rt.Cons != nil {
			out["consumer_handled"] = rt.Cons.Handled.Load()
			out["consumer_dupes"] = rt.Cons.Dupes.Load()
			out["consumer_errors"] = rt.Cons.Errors.Load()
		}
		httpx.WriteJSON(w, 200, out)
	})
	mux.HandleFunc("GET /v1/admin/outbox/quarantined", func(w http.ResponseWriter, r *http.Request) {
		corr := httpx.CorrelationID(r)
		if !requireOutboxRead(w, r) {
			return
		}
		if st == nil {
			httpx.WriteError(w, 503, "ORD_UNAVAILABLE", "store unavailable", corr, "failed", true)
			return
		}
		items, err := st.ListQuarantined(r.Context(), 100)
		if err != nil {
			httpx.WriteError(w, 500, "ORD_INTERNAL", err.Error(), corr, "failed", true)
			return
		}
		if items == nil {
			items = []store.QuarantinedEvent{}
		}
		httpx.WriteJSON(w, 200, map[string]any{"items": items})
	})
	mux.HandleFunc("POST /v1/admin/outbox/events/{event_id}/replay", func(w http.ResponseWriter, r *http.Request) {
		corr := httpx.CorrelationID(r)
		c, ok := authn.ClaimsFromContext(r.Context())
		if !ok || (c.PrincipalType != "operator" && c.PrincipalType != "service") {
			httpx.WriteError(w, 401, "ORD_UNAUTHORIZED", "operator/service bearer required", corr, "failed", false)
			return
		}
		if !authn.HasScope(c, "ops:outbox:replay") && !authn.HasScope(c, "*") {
			httpx.WriteError(w, 403, "ORD_FORBIDDEN", "ops:outbox:replay scope required", corr, "failed", false)
			return
		}
		if st == nil {
			httpx.WriteError(w, 503, "ORD_UNAVAILABLE", "store unavailable", corr, "failed", true)
			return
		}
		ev, err := st.RequestReplay(r.Context(), r.PathValue("event_id"), c.Subject, time.Now().UTC())
		if err == store.ErrNotFound {
			httpx.WriteError(w, 404, "ORD_OUTBOX_NOT_FOUND", "event not found", corr, "failed", false)
			return
		}
		if err != nil {
			httpx.WriteError(w, 409, "ORD_OUTBOX_REPLAY_CONFLICT", err.Error(), corr, "conflict", false)
			return
		}
		httpx.WriteJSON(w, 200, map[string]any{
			"event":  ev,
			"result": "queued_for_publish",
			"note":   "stable event_id preserved; publisher will claim when lease free",
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
	bgCancel()
	sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sh)
	slog.Info("stopped")
}

func refreshOrderGauges(ctx context.Context, st *store.Store) {
	age, n, err := st.OutboxBacklog(ctx)
	q := 0
	if qn, qerr := st.CountQuarantined(ctx); qerr == nil {
		q = qn
	}
	if err == nil {
		metrics.SetOutbox(float64(n), float64(q), age.Seconds())
	}
	recon, _ := st.ListByStatus(ctx, "RECONCILIATION_REQUIRED", 500)
	pending, _ := st.ListByStatus(ctx, "PAYMENT_PENDING", 500)
	metrics.SetOrderBacklog(float64(len(recon)), float64(len(pending)))
}

func requireOutboxRead(w http.ResponseWriter, r *http.Request) bool {
	corr := httpx.CorrelationID(r)
	c, ok := authn.ClaimsFromContext(r.Context())
	if !ok || (c.PrincipalType != "operator" && c.PrincipalType != "service") {
		httpx.WriteError(w, 401, "ORD_UNAUTHORIZED", "operator/service bearer required", corr, "failed", false)
		return false
	}
	if authn.HasScope(c, "ops:outbox:read") || authn.HasScope(c, "ops:outbox:replay") || authn.HasScope(c, "*") {
		return true
	}
	httpx.WriteError(w, 403, "ORD_FORBIDDEN", "ops:outbox:read or ops:outbox:replay required", corr, "failed", false)
	return false
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

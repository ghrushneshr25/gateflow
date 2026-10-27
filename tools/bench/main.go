// Gateflow P6.0 load / baseline harness.
// Synthetic local workloads only — not a production capacity certificate.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
)

type config struct {
	Scenario   string
	Root       string
	Sale       string
	Admission  string
	Order      string
	Inventory  string
	Concurrency int
	Duration   time.Duration
	Warmup     time.Duration
	Capacity   int
	Buyers     int
	OutPath    string
	Secret     string
	DBURL      string
	SkipSetup  bool
}

type latencyHist struct {
	mu   sync.Mutex
	vals []time.Duration
}

func (h *latencyHist) add(d time.Duration) {
	h.mu.Lock()
	h.vals = append(h.vals, d)
	h.mu.Unlock()
}

func (h *latencyHist) snapshot() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]time.Duration, len(h.vals))
	copy(out, h.vals)
	return out
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

type opStats struct {
	Name    string  `json:"name"`
	Count   int     `json:"count"`
	Errors  int     `json:"errors"`
	P50Ms   float64 `json:"p50_ms"`
	P95Ms   float64 `json:"p95_ms"`
	P99Ms   float64 `json:"p99_ms"`
	MaxMs   float64 `json:"max_ms"`
	MeanMs  float64 `json:"mean_ms"`
	RPS     float64 `json:"rps"`
}

func summarize(name string, h *latencyHist, errs int, wall time.Duration) opStats {
	vals := h.snapshot()
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	s := opStats{Name: name, Count: len(vals), Errors: errs}
	if len(vals) == 0 {
		return s
	}
	var sum time.Duration
	for _, v := range vals {
		sum += v
	}
	s.MeanMs = float64(sum) / float64(len(vals)) / 1e6
	s.P50Ms = float64(percentile(vals, 50)) / 1e6
	s.P95Ms = float64(percentile(vals, 95)) / 1e6
	s.P99Ms = float64(percentile(vals, 99)) / 1e6
	s.MaxMs = float64(vals[len(vals)-1]) / 1e6
	if wall > 0 {
		s.RPS = float64(len(vals)) / wall.Seconds()
	}
	return s
}

type result struct {
	Scenario    string            `json:"scenario"`
	Revision    string            `json:"revision"`
	StartedAt   time.Time         `json:"started_at"`
	FinishedAt  time.Time         `json:"finished_at"`
	WallSeconds float64           `json:"wall_seconds"`
	Params      map[string]any    `json:"params"`
	Environment map[string]string `json:"environment"`
	Setup       map[string]string `json:"setup,omitempty"`
	Ops         []opStats         `json:"ops"`
	Invariants  map[string]any    `json:"invariants"`
	// CheckoutOutcomes: mutually exclusive classification (P7.1). sum_classes == attempts.
	CheckoutOutcomes map[string]int64 `json:"checkout_outcomes,omitempty"`
	// Unexpected5xxSLO: provisional gate; false when unclassified remain.
	Unexpected5xxSLO map[string]any `json:"unexpected_5xx_slo,omitempty"`
	Notes            []string       `json:"notes"`
	OK               bool           `json:"ok"`
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Scenario, "scenario", "A", "A|B|C|D|E|F|G|all")
	flag.StringVar(&cfg.Root, "root", "", "repo root (default: detect)")
	flag.StringVar(&cfg.Sale, "sale", "http://127.0.0.1:8081", "")
	flag.StringVar(&cfg.Admission, "admission", "http://127.0.0.1:8082", "")
	flag.StringVar(&cfg.Order, "order", "http://127.0.0.1:8084", "")
	flag.StringVar(&cfg.Inventory, "inventory", "http://127.0.0.1:8083", "")
	flag.IntVar(&cfg.Concurrency, "c", 10, "concurrency")
	flag.DurationVar(&cfg.Duration, "d", 15*time.Second, "duration (steady scenarios)")
	flag.DurationVar(&cfg.Warmup, "warmup", 2*time.Second, "warmup")
	flag.IntVar(&cfg.Capacity, "capacity", 50, "ticket capacity for setup")
	flag.IntVar(&cfg.Buyers, "buyers", 0, "buyer count (default: concurrency*2)")
	flag.StringVar(&cfg.OutPath, "out", "", "JSON results path")
	flag.StringVar(&cfg.Secret, "secret", envOr("GATEFLOW_DEV_JWT_SECRET", authn.DefaultDevSecret), "")
	flag.StringVar(&cfg.DBURL, "db", envOr("DATABASE_URL", "postgres://gateflow:gateflow@localhost:5432/gateflow?sslmode=disable"), "")
	flag.BoolVar(&cfg.SkipSetup, "skip-setup", false, "reuse SES_ID/TT_ID/POOL_ID env")
	flag.Parse()
	if cfg.Root == "" {
		cfg.Root = detectRoot()
	}
	if cfg.Buyers <= 0 {
		cfg.Buyers = cfg.Concurrency * 2
	}

	scenarios := []string{cfg.Scenario}
	if cfg.Scenario == "all" {
		scenarios = []string{"A", "B", "C", "D", "E", "F", "G"}
	}

	rev := gitRev(cfg.Root)
	var exit int
	for _, sc := range scenarios {
		cfg.Scenario = sc
		res, err := runScenario(cfg, rev)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scenario %s ERROR: %v\n", sc, err)
			exit = 1
			continue
		}
		printSummary(res)
		if cfg.OutPath != "" {
			path := cfg.OutPath
			if len(scenarios) > 1 {
				dir := filepath.Dir(path)
				base := filepath.Base(path)
				path = filepath.Join(dir, fmt.Sprintf("%s-%s", sc, base))
			}
			if err := writeJSON(path, res); err != nil {
				fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
				exit = 1
			} else {
				fmt.Println("wrote", path)
			}
		}
		if !res.OK {
			exit = 1
		}
	}
	os.Exit(exit)
}

func runScenario(cfg config, rev string) (*result, error) {
	start := time.Now().UTC()
	res := &result{
		Scenario:  cfg.Scenario,
		Revision:  rev,
		StartedAt: start,
		Params: map[string]any{
			"concurrency": cfg.Concurrency,
			"duration":    cfg.Duration.String(),
			"warmup":      cfg.Warmup.String(),
			"capacity":    cfg.Capacity,
			"buyers":      cfg.Buyers,
		},
		Environment: map[string]string{
			"sale": cfg.Sale, "admission": cfg.Admission, "order": cfg.Order,
			"go": runtimeVersion(),
		},
		Invariants: map[string]any{},
		Notes:      []string{"synthetic local harness — not a production capacity certificate"},
		OK:         true,
	}

	client := &http.Client{Timeout: 20 * time.Second}
	opsTok, err := authn.MintOperatorJWT(cfg.Secret, "bench-ops", []string{authn.ScopeSaleWrite, authn.ScopeSalePublish}, time.Hour)
	if err != nil {
		return nil, err
	}
	svcTok, err := authn.MintServiceJWT(cfg.Secret, "bench-svc", []string{authn.ScopeAdmissionGrant, authn.ScopeAdmissionEntitlement}, time.Hour)
	if err != nil {
		return nil, err
	}

	var sesID, ttID, poolID string
	if cfg.SkipSetup {
		sesID = os.Getenv("SES_ID")
		ttID = os.Getenv("TT_ID")
		poolID = os.Getenv("POOL_ID")
		if sesID == "" || ttID == "" || poolID == "" {
			return nil, fmt.Errorf("skip-setup requires SES_ID TT_ID POOL_ID")
		}
	} else {
		cap := cfg.Capacity
		if cfg.Scenario == "D" && cap > cfg.Concurrency {
			// flash: scarce inventory relative to buyers
			cap = max(1, cfg.Concurrency/2)
		}
		var err error
		sesID, ttID, poolID, err = setupSale(client, cfg.Sale, opsTok, cap)
		if err != nil {
			return nil, fmt.Errorf("setup: %w", err)
		}
		res.Setup = map[string]string{"session_id": sesID, "ticket_type_id": ttID, "pool_id": poolID, "capacity": fmt.Sprint(cap)}
		res.Params["capacity"] = cap
	}

	switch cfg.Scenario {
	case "A":
		err = scenarioA(client, cfg, sesID, ttID, poolID, svcTok, res)
	case "B":
		err = scenarioB(client, cfg, sesID, ttID, poolID, svcTok, res)
	case "C":
		err = scenarioC(client, cfg, sesID, ttID, poolID, svcTok, res)
	case "D":
		err = scenarioD(client, cfg, sesID, ttID, poolID, svcTok, res)
	case "E":
		err = scenarioE(client, cfg, sesID, ttID, poolID, svcTok, res)
	case "F":
		err = scenarioF(client, cfg, sesID, ttID, poolID, svcTok, res)
	case "G":
		// Grant-only isolation (join+grant, no checkout) at stepped concurrency.
		err = scenarioG(client, cfg, sesID, svcTok, res)
	default:
		return nil, fmt.Errorf("unknown scenario %s", cfg.Scenario)
	}
	if err != nil {
		res.OK = false
		res.Notes = append(res.Notes, "scenario_error: "+err.Error())
	}

	// Invariant checks via SQL when possible
	inv := checkInvariants(cfg.DBURL, poolID, sesID)
	for k, v := range inv {
		res.Invariants[k] = v
	}
	if ok, _ := inv["ok"].(bool); !ok {
		res.OK = false
	}

	res.FinishedAt = time.Now().UTC()
	res.WallSeconds = res.FinishedAt.Sub(res.StartedAt).Seconds()
	return res, nil
}

func scenarioA(client *http.Client, cfg config, ses, tt, pool, svc string, res *result) error {
	// Steady baseline: sequential mix of browse + full journeys at concurrency
	return runConcurrentJourneys(client, cfg, ses, tt, pool, svc, res, cfg.Concurrency, cfg.Duration, true)
}

func scenarioB(client *http.Client, cfg config, ses, tt, pool, svc string, res *result) error {
	// Concurrent buyers + idempotent replay
	if err := runConcurrentJourneys(client, cfg, ses, tt, pool, svc, res, cfg.Concurrency, cfg.Duration, true); err != nil {
		return err
	}
	// Same idempotency key twice
	buyer := fmt.Sprintf("bench_idem_%d", time.Now().UnixNano())
	btok, _ := authn.MintBuyerJWT(cfg.Secret, buyer, nil, time.Hour)
	idem := "idem-bench-" + buyer
	hCheckout := &latencyHist{}
	var errs int
	for i := 0; i < 2; i++ {
		code, _, d, err := oneCheckout(client, cfg, ses, tt, pool, svc, buyer, btok, idem)
		hCheckout.add(d)
		if err != nil || (code >= 400 && code != 409) {
			// second may be 200 same order
			if code != 200 && code != 201 {
				errs++
			}
		}
	}
	res.Ops = append(res.Ops, summarize("idempotent_checkout_replay", hCheckout, errs, 0))
	res.Notes = append(res.Notes, "idempotent replay exercised twice with same key")
	return nil
}

func scenarioC(client *http.Client, cfg config, ses, tt, pool, svc string, res *result) error {
	// Step concurrency 1,2,5,10,20 (or up to -c)
	steps := []int{1, 2, 5, 10, 20}
	for _, c := range steps {
		if c > cfg.Concurrency && cfg.Concurrency > 0 {
			if c != steps[0] {
				break
			}
		}
		cc := c
		if cfg.Concurrency < c {
			cc = cfg.Concurrency
		}
		sub := *res
		sub.Ops = nil
		cfg2 := cfg
		cfg2.Concurrency = cc
		cfg2.Duration = 8 * time.Second
		cfg2.Warmup = time.Second
		if err := runConcurrentJourneys(client, cfg2, ses, tt, pool, svc, &sub, cc, cfg2.Duration, false); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("step c=%d error: %v", cc, err))
			continue
		}
		for _, op := range sub.Ops {
			op.Name = fmt.Sprintf("c%d_%s", cc, op.Name)
			res.Ops = append(res.Ops, op)
		}
		// Merge checkout outcome counters across spike steps.
		if sub.CheckoutOutcomes != nil {
			if res.CheckoutOutcomes == nil {
				res.CheckoutOutcomes = map[string]int64{}
			}
			for k, v := range sub.CheckoutOutcomes {
				if k == "sum_classes" {
					continue
				}
				res.CheckoutOutcomes[k] += v
			}
			var sum int64
			for _, name := range classOrder {
				sum += res.CheckoutOutcomes[name]
			}
			res.CheckoutOutcomes["sum_classes"] = sum
			pass, rate, reason := unexpected5xxSLO(res.CheckoutOutcomes, 0.01)
			res.Unexpected5xxSLO = map[string]any{
				"pass": pass, "rate": rate, "threshold": 0.01, "reason": reason,
			}
		}
		res.Notes = append(res.Notes, fmt.Sprintf("spike step concurrency=%d journeys=%d", cc, len(sub.Ops)))
	}
	return nil
}

func scenarioD(client *http.Client, cfg config, ses, tt, pool, svc string, res *result) error {
	// Flash: many buyers, scarce capacity (setup already reduced capacity)
	return runConcurrentJourneys(client, cfg, ses, tt, pool, svc, res, cfg.Concurrency, cfg.Duration, true)
}

func scenarioE(client *http.Client, cfg config, ses, tt, pool, svc string, res *result) error {
	// Async: run checkouts then sample outbox gauges / wait for lag drop
	if err := runConcurrentJourneys(client, cfg, ses, tt, pool, svc, res, max(2, cfg.Concurrency/2), 10*time.Second, false); err != nil {
		return err
	}
	// Poll order /metrics for outbox gauges
	h := &latencyHist{}
	deadline := time.Now().Add(30 * time.Second)
	var lastUnpub float64 = -1
	for time.Now().Before(deadline) {
		t0 := time.Now()
		m, err := scrapeMetrics(client, cfg.Order+"/metrics")
		h.add(time.Since(t0))
		if err == nil {
			if v, ok := m["gateflow_outbox_unpublished"]; ok {
				lastUnpub = v
				if v == 0 {
					break
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	res.Ops = append(res.Ops, summarize("metrics_scrape", h, 0, 0))
	res.Invariants["outbox_unpublished_end"] = lastUnpub
	if lastUnpub > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("outbox still unpublished=%.0f after wait", lastUnpub))
	}
	return nil
}

func resetAdmissionBudgets() {
	// Reset global + all session counters so stepped grant-only runs stay fail-open on budget.
	cmd := exec.Command("docker", "exec", "gateflow-postgres-1", "psql", "-U", "gateflow", "-d", "gateflow", "-c",
		"UPDATE admission.budget_scopes SET grants_issued=0, active_checkouts=0;")
	_ = cmd.Run()
}

func scenarioG(client *http.Client, cfg config, ses, svc string, res *result) error {
	// Isolate admission grant path: join + grant only, stepped concurrency.
	steps := []int{1, 5, 10, 20}
	for _, c := range steps {
		if cfg.Concurrency > 0 && c > cfg.Concurrency {
			continue
		}
		resetAdmissionBudgets()
		// Fresh session per concurrency step avoids session budget carry-over.
		opsTok, err := authn.MintOperatorJWT(cfg.Secret, "bench-ops", []string{authn.ScopeSaleWrite, authn.ScopeSalePublish}, time.Hour)
		if err != nil {
			return err
		}
		stepSes, _, _, err := setupSale(client, cfg.Sale, opsTok, max(cfg.Capacity, 100000))
		if err != nil {
			return fmt.Errorf("g setup c=%d: %w", c, err)
		}
		hJoin := &latencyHist{}
		hGrant := &latencyHist{}
		var eJoin, eGrant, okGrant atomic.Int64
		dur := 8 * time.Second
		wall0 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), dur)
		var wg sync.WaitGroup
		for w := 0; w < c; w++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				n := 0
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					buyer := fmt.Sprintf("gonly_%d_%d_%d", time.Now().UnixNano()%1e6, worker, n)
					n++
					btok, err := authn.MintBuyerJWT(cfg.Secret, buyer, nil, time.Hour)
					if err != nil {
						eGrant.Add(1)
						continue
					}
					t0 := time.Now()
					code, _, err := postJSON(client, cfg.Admission+"/v1/sessions/"+stepSes+"/queue/joins", map[string]string{
						"Authorization": "Bearer " + btok,
					}, map[string]any{})
					hJoin.add(time.Since(t0))
					if err != nil || code >= 400 {
						eJoin.Add(1)
						continue
					}
					t0 = time.Now()
					code, _, err = postJSON(client, cfg.Admission+"/v1/sessions/"+stepSes+"/grants", map[string]string{
						"Authorization": "Bearer " + svc,
					}, map[string]any{"buyer_id": buyer})
					hGrant.add(time.Since(t0))
					if err != nil || code >= 400 {
						eGrant.Add(1)
						continue
					}
					okGrant.Add(1)
				}
			}(w)
		}
		wg.Wait()
		cancel()
		wall := time.Since(wall0)
		res.Ops = append(res.Ops,
			summarize(fmt.Sprintf("c%d_join", c), hJoin, int(eJoin.Load()), wall),
			summarize(fmt.Sprintf("c%d_grant", c), hGrant, int(eGrant.Load()), wall),
		)
		res.Invariants[fmt.Sprintf("c%d_grants_ok", c)] = okGrant.Load()
		res.Invariants[fmt.Sprintf("c%d_grants_err", c)] = eGrant.Load()
		res.Notes = append(res.Notes, fmt.Sprintf("grant-only step c=%d ok=%d err=%d ses=%s", c, okGrant.Load(), eGrant.Load(), stepSes))
	}
	return nil
}

func scenarioF(client *http.Client, cfg config, ses, tt, pool, svc string, res *result) error {
	// Recovery baseline: health check latency + one journey before/after brief pause
	// (full process kill is crash harness; here we measure restart-adjacent: dependency readiness)
	hHealth := &latencyHist{}
	var herrs int
	for i := 0; i < 20; i++ {
		t0 := time.Now()
		code, err := getCode(client, cfg.Order+"/healthz")
		hHealth.add(time.Since(t0))
		if err != nil || code != 200 {
			herrs++
		}
	}
	res.Ops = append(res.Ops, summarize("order_healthz", hHealth, herrs, 0))
	if err := runConcurrentJourneys(client, cfg, ses, tt, pool, svc, res, 2, 5*time.Second, false); err != nil {
		return err
	}
	res.Notes = append(res.Notes, "full process-kill recovery covered by make test-crash-recovery (not re-run inside bench F)")
	return nil
}

func runConcurrentJourneys(client *http.Client, cfg config, ses, tt, pool, svc string, res *result, conc int, dur time.Duration, doWarmup bool) error {
	if conc < 1 {
		conc = 1
	}
	hBrowse := &latencyHist{}
	hJoin := &latencyHist{}
	hGrant := &latencyHist{}
	hCheckout := &latencyHist{}
	var eBrowse, eJoin, eGrant, eCheckout atomic.Int64
	var okCheckout, failCheckout, otherCheckout atomic.Int64
	var outcomes outcomeCounters

	if doWarmup && cfg.Warmup > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Warmup)
		runWorkers(ctx, client, cfg, ses, tt, pool, svc, conc, hBrowse, hJoin, hGrant, hCheckout, &eBrowse, &eJoin, &eGrant, &eCheckout, &okCheckout, &failCheckout, &otherCheckout, &outcomes)
		cancel()
		// reset histograms after warmup
		*hBrowse, *hJoin, *hGrant, *hCheckout = latencyHist{}, latencyHist{}, latencyHist{}, latencyHist{}
		eBrowse.Store(0)
		eJoin.Store(0)
		eGrant.Store(0)
		eCheckout.Store(0)
		okCheckout.Store(0)
		failCheckout.Store(0)
		otherCheckout.Store(0)
		outcomes = outcomeCounters{}
	}

	wall0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	runWorkers(ctx, client, cfg, ses, tt, pool, svc, conc, hBrowse, hJoin, hGrant, hCheckout, &eBrowse, &eJoin, &eGrant, &eCheckout, &okCheckout, &failCheckout, &otherCheckout, &outcomes)
	cancel()
	wall := time.Since(wall0)

	res.Ops = append(res.Ops,
		summarize("browse_offerings", hBrowse, int(eBrowse.Load()), wall),
		summarize("queue_join", hJoin, int(eJoin.Load()), wall),
		summarize("grant", hGrant, int(eGrant.Load()), wall),
		summarize("checkout", hCheckout, int(eCheckout.Load()), wall),
	)
	res.Invariants["checkouts_http_err"] = eCheckout.Load()
	res.Invariants["checkouts_confirmed"] = okCheckout.Load()
	res.Invariants["checkouts_failed_status"] = failCheckout.Load()
	res.Invariants["checkouts_other_status"] = otherCheckout.Load()
	snap := outcomes.snapshot()
	res.CheckoutOutcomes = snap
	pass, rate, reason := unexpected5xxSLO(snap, 0.01)
	res.Unexpected5xxSLO = map[string]any{
		"pass": pass, "rate": rate, "threshold": 0.01, "reason": reason,
	}
	if !pass {
		res.Notes = append(res.Notes, "unexpected_5xx_slo: "+reason)
	}
	return nil
}

func runWorkers(ctx context.Context, client *http.Client, cfg config, ses, tt, pool, svc string,
	conc int, hBrowse, hJoin, hGrant, hCheckout *latencyHist,
	eBrowse, eJoin, eGrant, eCheckout, okCheckout, failCheckout, otherCheckout *atomic.Int64,
	outcomes *outcomeCounters) {
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			n := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				buyer := fmt.Sprintf("bench_b%d_w%d_%d", time.Now().UnixNano()%1_000_000, worker, n)
				n++
				btok, err := authn.MintBuyerJWT(cfg.Secret, buyer, nil, time.Hour)
				if err != nil {
					eCheckout.Add(1)
					if outcomes != nil {
						outcomes.add(ClassUnclassified)
					}
					continue
				}
				// browse
				t0 := time.Now()
				code, err := getCode(client, cfg.Sale+"/v1/offerings")
				hBrowse.add(time.Since(t0))
				if err != nil || code >= 400 {
					eBrowse.Add(1)
				}
				// join
				t0 = time.Now()
				code, _, err = postJSON(client, cfg.Admission+"/v1/sessions/"+ses+"/queue/joins", map[string]string{
					"Authorization": "Bearer " + btok,
				}, map[string]any{})
				hJoin.add(time.Since(t0))
				if err != nil || code >= 400 {
					eJoin.Add(1)
					continue
				}
				// grant
				t0 = time.Now()
				code, body, err := postJSON(client, cfg.Admission+"/v1/sessions/"+ses+"/grants", map[string]string{
					"Authorization": "Bearer " + svc,
				}, map[string]any{"buyer_id": buyer})
				hGrant.add(time.Since(t0))
				if err != nil || code >= 400 {
					eGrant.Add(1)
					continue
				}
				ent := digString(body, "entitlement", "id")
				if ent == "" {
					eGrant.Add(1)
					continue
				}
				// checkout
				idem := fmt.Sprintf("idem-%s", buyer)
				t0 = time.Now()
				code, body, err = postJSON(client, cfg.Order+"/v1/checkouts", map[string]string{
					"Authorization":   "Bearer " + btok,
					"Idempotency-Key": idem,
					"Content-Type":    "application/json",
				}, map[string]any{
					"session_id":      ses,
					"ticket_type_id":  tt,
					"pool_id":         pool,
					"entitlement_id":  ent,
					"quantity":        1,
					"amount_cents":    1000,
					"idempotency_key": idem,
				})
				hCheckout.add(time.Since(t0))
				class := classifyCheckout(code, body, err)
				if outcomes != nil {
					outcomes.add(class)
				}
				if legacyHTTPErr(class) {
					eCheckout.Add(1)
					continue
				}
				switch class {
				case ClassBizConfirmed:
					okCheckout.Add(1)
				case ClassBizFailedStatus:
					failCheckout.Add(1)
				default:
					otherCheckout.Add(1)
				}
			}
		}(i)
	}
	wg.Wait()
}

func oneCheckout(client *http.Client, cfg config, ses, tt, pool, svc, buyer, btok, idem string) (int, map[string]any, time.Duration, error) {
	t0 := time.Now()
	_, _, _ = postJSON(client, cfg.Admission+"/v1/sessions/"+ses+"/queue/joins", map[string]string{
		"Authorization": "Bearer " + btok,
	}, map[string]any{})
	code, body, err := postJSON(client, cfg.Admission+"/v1/sessions/"+ses+"/grants", map[string]string{
		"Authorization": "Bearer " + svc,
	}, map[string]any{"buyer_id": buyer})
	if err != nil || code >= 400 {
		return code, body, time.Since(t0), err
	}
	ent := digString(body, "entitlement", "id")
	code, body, err = postJSON(client, cfg.Order+"/v1/checkouts", map[string]string{
		"Authorization":   "Bearer " + btok,
		"Idempotency-Key": idem,
	}, map[string]any{
		"session_id": ses, "ticket_type_id": tt, "pool_id": pool, "entitlement_id": ent,
		"quantity": 1, "amount_cents": 1000, "idempotency_key": idem,
	})
	return code, body, time.Since(t0), err
}

func setupSale(client *http.Client, saleBase, opsTok string, capacity int) (ses, tt, pool string, err error) {
	h := map[string]string{"Authorization": "Bearer " + opsTok, "Content-Type": "application/json"}
	code, body, err := postJSON(client, saleBase+"/v1/offerings", h, map[string]any{
		"name": "bench-" + time.Now().Format("150405"), "category": "concert",
	})
	if err != nil || code >= 400 {
		return "", "", "", fmt.Errorf("offering %d %v %v", code, body, err)
	}
	off := digString(body, "id")
	now := time.Now().UTC()
	code, body, err = postJSON(client, saleBase+"/v1/offerings/"+off+"/sessions", h, map[string]any{
		"starts_at": now.Add(48 * time.Hour).Format(time.RFC3339),
		"ends_at":   now.Add(52 * time.Hour).Format(time.RFC3339),
		"sale_opens_at": now.Add(-time.Hour).Format(time.RFC3339),
		"sale_closes_at": now.Add(24 * time.Hour).Format(time.RFC3339),
		"max_per_buyer": 4,
	})
	if err != nil || code >= 400 {
		return "", "", "", fmt.Errorf("session %d %v", code, body)
	}
	ses = digString(body, "session", "id")
	code, body, err = postJSON(client, saleBase+"/v1/sessions/"+ses+"/ticket-types", h, map[string]any{
		"name": "GA", "capacity": capacity, "price_cents": 1000,
	})
	if err != nil || code >= 400 {
		return "", "", "", fmt.Errorf("tt %d %v", code, body)
	}
	tt = digString(body, "id")
	code, body, err = postJSON(client, saleBase+"/v1/offerings/"+off+"/publish", h, map[string]any{})
	if err != nil || code >= 400 {
		return "", "", "", fmt.Errorf("publish %d %v", code, body)
	}
	// ticket_types[0].inventory_pool_id
	if tts, ok := body["ticket_types"].([]any); ok && len(tts) > 0 {
		if m, ok := tts[0].(map[string]any); ok {
			pool, _ = m["inventory_pool_id"].(string)
		}
	}
	if pool == "" {
		return "", "", "", fmt.Errorf("no pool in publish: %v", body)
	}
	return ses, tt, pool, nil
}

func checkInvariants(dbURL, poolID, sessionID string) map[string]any {
	out := map[string]any{"ok": true, "checked": false}
	// Use docker exec psql to avoid adding pgx dep to bench module if docker available
	if dbURL == "" {
		out["note"] = "no db url"
		return out
	}
	// -c ON_ERROR_STOP=1 -v ON_ERROR_STOP; use -c for search_path to avoid "SET" row in -t output
	pid := strings.ReplaceAll(poolID, "'", "")
	// Authoritative pool counters + DB CHECK (confirmed+active <= sellable_capacity).
	sql := fmt.Sprintf(`SELECT sellable_capacity, confirmed_qty, active_reserved_qty,
  (confirmed_qty + active_reserved_qty) AS used
FROM inventory.pools WHERE id='%s';`, pid)
	cmd := exec.Command("docker", "exec", "-i", "gateflow-postgres-1",
		"psql", "-U", "gateflow", "-d", "gateflow", "-t", "-A", "-F", ",", "-c", sql)
	b, err := cmd.CombinedOutput()
	if err != nil {
		out["db_error"] = err.Error() + " " + string(b)
		out["note"] = "invariant SQL via docker failed; mark unavailable not zero"
		return out
	}
	line := strings.TrimSpace(string(b))
	for _, ln := range strings.Split(line, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			line = ln
			break
		}
	}
	parts := strings.Split(line, ",")
	if len(parts) >= 4 {
		out["checked"] = true
		out["sellable_capacity"] = parts[0]
		out["confirmed_qty"] = parts[1]
		out["active_reserved_qty"] = parts[2]
		out["used_qty"] = parts[3]
		var capv, conf, act int
		fmt.Sscanf(parts[0], "%d", &capv)
		fmt.Sscanf(parts[1], "%d", &conf)
		fmt.Sscanf(parts[2], "%d", &act)
		okInv := conf+act <= capv
		out["invariant_inventory"] = okInv
		if !okInv {
			out["ok"] = false
			out["oversell"] = true
		}
	} else {
		out["raw"] = line
		out["note"] = "unexpected SQL shape"
	}
	return out
}

func scrapeMetrics(client *http.Client, url string) (map[string]float64, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	out := map[string]float64{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		// simple: name{...} value OR name value
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if i := strings.IndexByte(name, '{'); i >= 0 {
			name = name[:i]
		}
		var v float64
		if _, err := fmt.Sscanf(fields[len(fields)-1], "%f", &v); err == nil {
			out[name] = v
		}
	}
	return out, nil
}

func postJSON(client *http.Client, url string, headers map[string]string, body any) (int, map[string]any, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{"_raw": string(b)}
	}
	return resp.StatusCode, m, nil
}

func getCode(client *http.Client, url string) (int, error) {
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func digString(m map[string]any, keys ...string) string {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return s
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func printSummary(res *result) {
	fmt.Printf("\n=== Scenario %s ok=%v wall=%.1fs rev=%s ===\n", res.Scenario, res.OK, res.WallSeconds, res.Revision)
	for _, op := range res.Ops {
		fmt.Printf("  %-28s n=%4d err=%3d p50=%7.1fms p95=%7.1fms p99=%7.1fms rps=%6.1f\n",
			op.Name, op.Count, op.Errors, op.P50Ms, op.P95Ms, op.P99Ms, op.RPS)
	}
	fmt.Printf("  invariants: %v\n", res.Invariants)
	if len(res.CheckoutOutcomes) > 0 {
		fmt.Printf("  checkout_outcomes: attempts=%d sum=%d confirmed=%d failed_status=%d http_5xx=%d timeout=%d transport=%d unclassified=%d\n",
			res.CheckoutOutcomes["attempts"],
			res.CheckoutOutcomes["sum_classes"],
			res.CheckoutOutcomes[ClassBizConfirmed],
			res.CheckoutOutcomes[ClassBizFailedStatus],
			res.CheckoutOutcomes[ClassHTTP5xx],
			res.CheckoutOutcomes[ClassClientTimeout],
			res.CheckoutOutcomes[ClassTransportError],
			res.CheckoutOutcomes[ClassUnclassified],
		)
	}
	if res.Unexpected5xxSLO != nil {
		fmt.Printf("  unexpected_5xx_slo: pass=%v rate=%v reason=%v\n",
			res.Unexpected5xxSLO["pass"], res.Unexpected5xxSLO["rate"], res.Unexpected5xxSLO["reason"])
	}
	for _, n := range res.Notes {
		fmt.Printf("  note: %s\n", n)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func detectRoot() string {
	wd, _ := os.Getwd()
	for d := wd; d != "/" && d != ""; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.work")); err == nil {
			return d
		}
	}
	return wd
}

func gitRev(root string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

func runtimeVersion() string {
	cmd := exec.Command("go", "version")
	b, _ := cmd.Output()
	return strings.TrimSpace(string(b))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

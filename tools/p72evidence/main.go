// P72 evidence-index validator (contract enforcement helper).
// Does not execute staging. Refuses overall PASS without shared_staging gates.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type indexFile struct {
	SchemaVersion       string   `json:"schema_version"`
	OverallStatus       string   `json:"overall_status"`
	ProtocolBEnabled    bool     `json:"protocol_b_enabled"`
	ProductionAuthorized bool    `json:"production_authorized"`
	HardStopsObserved   []string `json:"hard_stops_observed"`
	Gates               []gate   `json:"gates"`
}

type gate struct {
	GateID            string         `json:"gate_id"`
	Title             string         `json:"title"`
	Status            string         `json:"status"`
	EnvironmentID     string         `json:"environment_id"`
	EnvironmentClass  string         `json:"environment_class"`
	GitSHA            string         `json:"git_sha"`
	ImageDigests      map[string]any `json:"image_digests"`
	StartedAtUTC      string         `json:"started_at_utc"`
	FinishedAtUTC     string         `json:"finished_at_utc"`
	ArtifactPaths     []string       `json:"artifact_paths"`
	ObservedResult    string         `json:"observed_result"`
	Owner             string         `json:"owner"`
	HardStopTriggered bool           `json:"hard_stop_triggered"`
	Mandatory         bool           `json:"mandatory"`
}

type checkoutOutcomes struct {
	Attempts      int64            `json:"attempts"`
	SumClasses    int64            `json:"sum_classes"`
	HTTP5xx       int64            `json:"http_5xx"`
	Unclassified  int64            `json:"unclassified"`
	Extra         map[string]int64 `json:"-"`
}

func main() {
	indexPath := flag.String("index", "docs/phase-7/results/P72-evidence-index.json", "evidence index path")
	perfJSON := flag.String("perf", "", "optional bench JSON with checkout_outcomes to check 5xx SLO")
	flag.Parse()

	raw, err := os.ReadFile(*indexPath)
	if err != nil {
		fatalf("read index: %v", err)
	}
	var idx indexFile
	if err := json.Unmarshal(raw, &idx); err != nil {
		fatalf("parse index: %v", err)
	}

	var errs []string
	if idx.ProtocolBEnabled {
		errs = append(errs, "protocol_b_enabled must be false")
	}
	if idx.ProductionAuthorized {
		errs = append(errs, "production_authorized must be false for P7.2 index")
	}

	requiredIDs := []string{
		"G1_deployment_identity",
		"G2_buyer_journey_security",
		"G3_inventory_budget_fence",
		"G4_reservation_payment_recovery",
		"G5_outbox_kafka_crash",
		"G6_performance_classified",
		"G7_observability_alerting",
		"G8_artifact_rollback",
		"G9_operator_readiness",
		"G10_evidence_integrity_verdict",
	}
	seen := map[string]bool{}
	for _, g := range idx.Gates {
		seen[g.GateID] = true
		errs = append(errs, validateGate(g)...)
	}
	for _, id := range requiredIDs {
		if !seen[id] {
			errs = append(errs, fmt.Sprintf("missing mandatory gate_id %s", id))
		}
	}

	// Overall PASS rules
	if strings.EqualFold(idx.OverallStatus, "PASS") {
		if len(idx.HardStopsObserved) > 0 {
			errs = append(errs, "overall PASS forbidden when hard_stops_observed is non-empty")
		}
		for _, g := range idx.Gates {
			if g.Mandatory && !strings.EqualFold(g.Status, "PASS") {
				errs = append(errs, fmt.Sprintf("overall PASS requires gate %s status PASS (have %s)", g.GateID, g.Status))
			}
			if g.HardStopTriggered {
				errs = append(errs, fmt.Sprintf("overall PASS forbidden: gate %s hard_stop_triggered", g.GateID))
			}
		}
	}

	if *perfJSON != "" {
		errs = append(errs, validatePerfOutcomes(*perfJSON)...)
	}

	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "P72 evidence validation FAILED (%d issues)\n", len(errs))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		os.Exit(1)
	}
	fmt.Printf("P72 evidence validation OK index=%s overall_status=%s gates=%d\n", *indexPath, idx.OverallStatus, len(idx.Gates))
}

func validateGate(g gate) []string {
	var errs []string
	st := strings.ToUpper(g.Status)
	switch st {
	case "PASS", "FAIL", "BLOCKED", "NOT_RUN", "NOT_VERIFIED", "CONDITIONAL":
	default:
		errs = append(errs, fmt.Sprintf("%s: invalid status %q", g.GateID, g.Status))
	}
	if st != "PASS" {
		return errs
	}
	// PASS requires full metadata + shared staging
	if g.EnvironmentClass != "shared_staging" {
		errs = append(errs, fmt.Sprintf("%s: PASS requires environment_class=shared_staging (have %q)", g.GateID, g.EnvironmentClass))
	}
	if strings.TrimSpace(g.GitSHA) == "" {
		errs = append(errs, fmt.Sprintf("%s: PASS requires git_sha", g.GateID))
	}
	if strings.TrimSpace(g.StartedAtUTC) == "" || strings.TrimSpace(g.FinishedAtUTC) == "" {
		errs = append(errs, fmt.Sprintf("%s: PASS requires started_at_utc and finished_at_utc", g.GateID))
	}
	if len(g.ArtifactPaths) < 1 {
		errs = append(errs, fmt.Sprintf("%s: PASS requires artifact_paths", g.GateID))
	}
	if strings.TrimSpace(g.Owner) == "" {
		errs = append(errs, fmt.Sprintf("%s: PASS requires owner", g.GateID))
	}
	if strings.TrimSpace(g.ObservedResult) == "" {
		errs = append(errs, fmt.Sprintf("%s: PASS requires observed_result", g.GateID))
	}
	if g.HardStopTriggered {
		errs = append(errs, fmt.Sprintf("%s: PASS forbidden when hard_stop_triggered", g.GateID))
	}
	return errs
}

func validatePerfOutcomes(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("perf read: %v", err)}
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return []string{fmt.Sprintf("perf parse: %v", err)}
	}
	co, ok := root["checkout_outcomes"].(map[string]any)
	if !ok {
		return []string{"perf JSON missing checkout_outcomes"}
	}
	attempts := int64(asFloat(co["attempts"]))
	sum := int64(asFloat(co["sum_classes"]))
	n5 := int64(asFloat(co["http_5xx"]))
	un := int64(asFloat(co["unclassified"]))
	var errs []string
	if attempts <= 0 {
		errs = append(errs, "perf: attempts must be > 0")
	}
	if sum != attempts {
		errs = append(errs, fmt.Sprintf("perf: sum_classes %d != attempts %d", sum, attempts))
	}
	if un > 0 {
		errs = append(errs, fmt.Sprintf("perf: unclassified=%d blocks 5xx SLO PASS", un))
	}
	if attempts > 0 {
		rate := float64(n5) / float64(attempts)
		if rate >= 0.01 {
			errs = append(errs, fmt.Sprintf("perf: http_5xx rate %.4f >= 0.01", rate))
		}
	}
	// unexpected_5xx_slo.pass must not be true if rules fail
	if slo, ok := root["unexpected_5xx_slo"].(map[string]any); ok {
		if pass, _ := slo["pass"].(bool); pass && len(errs) > 0 {
			errs = append(errs, "perf: unexpected_5xx_slo.pass=true but outcomes violate rules")
		}
		if pass, _ := slo["pass"].(bool); pass && un > 0 {
			errs = append(errs, "perf: unexpected_5xx_slo cannot PASS with unclassified>0")
		}
	}
	return errs
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func fatalf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(2)
}

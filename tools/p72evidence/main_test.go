package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateGate_PassRequiresSharedStaging(t *testing.T) {
	g := gate{
		GateID:           "G1_deployment_identity",
		Status:           "PASS",
		EnvironmentClass: "local_lab",
		GitSHA:           "abc",
		StartedAtUTC:     "2026-01-01T00:00:00Z",
		FinishedAtUTC:    "2026-01-01T01:00:00Z",
		ArtifactPaths:    []string{"x"},
		Owner:            "r",
		ObservedResult:   "ok",
	}
	errs := validateGate(g)
	if len(errs) == 0 {
		t.Fatal("expected failure for local_lab PASS")
	}
}

func TestValidateGate_PassOK(t *testing.T) {
	g := gate{
		GateID:           "G1_deployment_identity",
		Status:           "PASS",
		EnvironmentClass: "shared_staging",
		GitSHA:           "abc",
		StartedAtUTC:     "2026-01-01T00:00:00Z",
		FinishedAtUTC:    "2026-01-01T01:00:00Z",
		ArtifactPaths:    []string{"x"},
		Owner:            "r",
		ObservedResult:   "ok",
	}
	if errs := validateGate(g); len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
}

func TestValidatePerfOutcomes_UnclassifiedBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	body := map[string]any{
		"checkout_outcomes": map[string]any{
			"attempts": 10, "sum_classes": 10, "http_5xx": 0, "unclassified": 1,
			"biz_confirmed": 9,
		},
		"unexpected_5xx_slo": map[string]any{"pass": true},
	}
	b, _ := json.Marshal(body)
	_ = os.WriteFile(path, b, 0o644)
	errs := validatePerfOutcomes(path)
	if len(errs) == 0 {
		t.Fatal("expected unclassified to fail")
	}
}

func TestValidatePerfOutcomes_5xxRate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	body := map[string]any{
		"checkout_outcomes": map[string]any{
			"attempts": 100, "sum_classes": 100, "http_5xx": 5, "unclassified": 0,
		},
	}
	b, _ := json.Marshal(body)
	_ = os.WriteFile(path, b, 0o644)
	errs := validatePerfOutcomes(path)
	if len(errs) == 0 {
		t.Fatal("expected 5xx rate fail")
	}
}

func TestValidatePerfOutcomes_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	body := map[string]any{
		"checkout_outcomes": map[string]any{
			"attempts": 100, "sum_classes": 100, "http_5xx": 0, "unclassified": 0,
			"biz_confirmed": 100,
		},
		"unexpected_5xx_slo": map[string]any{"pass": true, "rate": 0},
	}
	b, _ := json.Marshal(body)
	_ = os.WriteFile(path, b, 0o644)
	if errs := validatePerfOutcomes(path); len(errs) != 0 {
		t.Fatalf("%v", errs)
	}
}

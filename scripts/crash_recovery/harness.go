//go:build ignore

// Standalone harness runner: go run scripts/crash_recovery/harness.go
// Prefer: go test -tags='integration crash' ./services/order/tests/crash/
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("go", "test", "-tags=integration,crash", "-count=1", "-timeout", "10m", "-v", "./services/order/tests/crash/")
	cmd.Dir = mustFindRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+envOr("DATABASE_URL", "postgres://gateflow:gateflow@localhost:5432/gateflow?sslmode=disable"),
		"KAFKA_BROKERS="+envOr("KAFKA_BROKERS", "localhost:9092"),
	)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "crash recovery failed: %v\n", err)
		os.Exit(1)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func mustFindRoot() string {
	wd, _ := os.Getwd()
	for dir := wd; dir != "/" && dir != ""; dir = parent(dir) {
		if _, err := os.Stat(dir + "/go.work"); err == nil {
			return dir
		}
	}
	return wd
}

func parent(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return ""
}

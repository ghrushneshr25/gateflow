//go:build integration && crash

package crash

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Proc struct {
	Name    string
	Cmd     *exec.Cmd
	LogPath string
	Port    int
	Bin     string
	Env     []string
	dead    bool
}

func repoRoot() string {
	wd, _ := os.Getwd()
	// tests run from package dir
	for dir := wd; len(dir) > 1; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
	}
	return wd
}

func startProc(name, bin string, port int, extraEnv []string, logPath string) (*Proc, error) {
	root := repoRoot()
	binPath := filepath.Join(root, "bin", bin)
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("missing binary %s (run make build): %w", binPath, err)
	}
	if err := requirePortFree(port); err != nil {
		return nil, fmt.Errorf("%s: %w (stop the conflicting host process, then re-run crash suite)", name, err)
	}
	logF, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binPath)
	cmd.Dir = root
	cmd.Stdout = logF
	cmd.Stderr = logF
	// Later entries win: crash harness overrides must beat parent process env
	// (Go uses the first value for duplicate keys if we prepended os.Environ).
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return nil, err
	}
	p := &Proc{Name: name, Cmd: cmd, LogPath: logPath, Port: port, Bin: bin, Env: extraEnv}
	if err := waitReady(p, port, 30*time.Second); err != nil {
		alive := p.Alive()
		logTail := readLogTail(logPath, 4000)
		_ = p.Kill()
		return nil, fmt.Errorf("%w alive=%v log=%s", err, alive, logTail)
	}
	return p, nil
}

func readLogTail(path string, max int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	if len(b) > max {
		b = b[len(b)-max:]
	}
	return string(b)
}

// mergeEnv returns a copy of base with override key/value pairs winning on duplicate keys.
func mergeEnv(base, override []string) []string {
	type kv struct{ k, v string }
	order := make([]string, 0, len(base)+len(override))
	m := map[string]string{}
	put := func(e string) {
		k, v, ok := splitEnv(e)
		if !ok {
			return
		}
		if _, exists := m[k]; !exists {
			order = append(order, k)
		}
		m[k] = v
	}
	for _, e := range base {
		put(e)
	}
	for _, e := range override {
		put(e)
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+m[k])
	}
	return out
}

func splitEnv(e string) (string, string, bool) {
	for i := 0; i < len(e); i++ {
		if e[i] == '=' {
			return e[:i], e[i+1:], true
		}
	}
	return "", "", false
}

// requirePortFree fails fast when another process already listens on port.
// Prevents crash harness from attaching to a foreign order/sale instance (misleading barriers).
func requirePortFree(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	c, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
	if err != nil {
		// connection refused / nothing listening → free
		return nil
	}
	_ = c.Close()
	return fmt.Errorf("port %d already in use (expected free for crash isolation)", port)
}

// waitReady waits until healthz is up, or the process exits after hitting a crash barrier
// (consumer crash can beat Listen under load — barrier file means start succeeded for crash tests).
func waitReady(p *Proc, port int, d time.Duration) error {
	deadline := time.Now().Add(d)
	client := &http.Client{Timeout: 300 * time.Millisecond}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	hookDir := envFrom(p.Env, "GATEFLOW_HOOK_DIR")
	boundary := envFrom(p.Env, "GATEFLOW_CRASH_BOUNDARY")
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		if !p.Alive() {
			if hookDir != "" && boundary != "" {
				if _, statErr := os.Stat(filepath.Join(hookDir, "reached_"+boundary)); statErr == nil {
					// Intentional crash-barrier exit before/without stable healthz.
					return nil
				}
			}
			return fmt.Errorf("port %d not ready: process exited before healthz", port)
		}
		if tcpOpen(fmt.Sprintf("127.0.0.1:%d", port)) || tcpOpen(fmt.Sprintf("localhost:%d", port)) {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("port %d not ready", port)
}

func envFrom(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if len(env[i]) >= len(prefix) && env[i][:len(prefix)] == prefix {
			return env[i][len(prefix):]
		}
	}
	return ""
}

func tcpOpen(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func waitFile(path string, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("file not created: %s", path)
}

func (p *Proc) Kill() error {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return nil
	}
	_ = p.Cmd.Process.Signal(syscall.SIGKILL)
	_, _ = p.Cmd.Process.Wait()
	p.dead = true
	return nil
}

func (p *Proc) Alive() bool {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil || p.dead {
		return false
	}
	if p.Cmd.ProcessState != nil && p.Cmd.ProcessState.Exited() {
		p.dead = true
		return false
	}
	// Reap zombie without blocking; Signal(0) alone can look "alive" for zombies.
	var wstatus syscall.WaitStatus
	wpid, _ := syscall.Wait4(p.Cmd.Process.Pid, &wstatus, syscall.WNOHANG, nil)
	if wpid == p.Cmd.Process.Pid {
		p.dead = true
		return false
	}
	err := p.Cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

func (p *Proc) Restart(extraEnv []string) error {
	port := p.Port
	_ = p.Kill()
	if err := waitPortFree(port, 10*time.Second); err != nil {
		return fmt.Errorf("restart %s: %w", p.Name, err)
	}
	env := append([]string{}, p.Env...)
	// strip crash hooks on restart unless provided
	dropPrefixes := []string{
		"GATEFLOW_CRASH_BOUNDARY=",
		"GATEFLOW_HOLD_AFTER_SIGNAL=",
		"GATEFLOW_CRASH_EVENT_FILTER=",
		"GATEFLOW_OUTBOX_PUBLISHER=",
		"GATEFLOW_OUTBOX_CONSUMER=",
		"GATEFLOW_KAFKA_GROUP=",
	}
	cleaned := make([]string, 0, len(env))
	for _, e := range env {
		drop := false
		for _, pref := range dropPrefixes {
			if len(e) >= len(pref) && e[:len(pref)] == pref {
				drop = true
				break
			}
		}
		if !drop {
			cleaned = append(cleaned, e)
		}
	}
	cleaned = append(cleaned, extraEnv...)
	np, err := startProc(p.Name, p.Bin, port, cleaned, p.LogPath+".restart")
	if err != nil {
		return err
	}
	*p = *np
	return nil
}

func waitPortFree(port int, d time.Duration) error {
	deadline := time.Now().Add(d)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return nil
		}
		_ = c.Close()
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("port %d still in use after kill", port)
}

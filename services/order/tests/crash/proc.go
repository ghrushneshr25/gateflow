//go:build integration && crash

package crash

import (
	"fmt"
	"net"
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
	logF, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binPath)
	cmd.Dir = root
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Env = append(os.Environ(), extraEnv...)
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return nil, err
	}
	p := &Proc{Name: name, Cmd: cmd, LogPath: logPath, Port: port, Bin: bin, Env: extraEnv}
	if err := waitPort(port, 30*time.Second); err != nil {
		_ = p.Kill()
		return nil, err
	}
	return p, nil
}

func waitPort(port int, d time.Duration) error {
	deadline := time.Now().Add(d)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("port %d not ready", port)
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
	return nil
}

func (p *Proc) Alive() bool {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return false
	}
	err := p.Cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

func (p *Proc) Restart(extraEnv []string) error {
	_ = p.Kill()
	time.Sleep(200 * time.Millisecond)
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
		for _, p := range dropPrefixes {
			if len(e) >= len(p) && e[:len(p)] == p {
				drop = true
				break
			}
		}
		if !drop {
			cleaned = append(cleaned, e)
		}
	}
	cleaned = append(cleaned, extraEnv...)
	np, err := startProc(p.Name, p.Bin, p.Port, cleaned, p.LogPath+".restart")
	if err != nil {
		return err
	}
	*p = *np
	return nil
}

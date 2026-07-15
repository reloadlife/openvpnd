package ovpnbackend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HostOptions configures the host backend.
type HostOptions struct {
	ConfDir    string
	RuntimeDir string
	AllowHooks bool
}

// HostBackend manages real openvpn processes.
type HostBackend struct {
	opts    HostOptions
	mu      sync.Mutex
	managed map[string]*procState // name -> state
}

type procState struct {
	desired DesiredInstance
	cmd     *exec.Cmd
	pid     int
}

// NewHostBackend creates a host backend.
func NewHostBackend(opts HostOptions) (*HostBackend, error) {
	if opts.ConfDir == "" {
		opts.ConfDir = "/etc/openvpnd/instances"
	}
	if opts.RuntimeDir == "" {
		opts.RuntimeDir = "/run/openvpnd"
	}
	for _, d := range []string{opts.ConfDir, opts.RuntimeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return &HostBackend{
		opts:    opts,
		managed: make(map[string]*procState),
	}, nil
}

func (b *HostBackend) ProbeBinary(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty binary path")
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// openvpn --version often exits 1; still prints version
		if len(out) == 0 {
			return "", fmt.Errorf("probe %s: %w", path, err)
		}
	}
	return firstVersionLine(string(out)), nil
}

func (b *HostBackend) ListLive(ctx context.Context) ([]LiveInstance, error) {
	_ = ctx
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]LiveInstance, 0, len(b.managed))
	for name, st := range b.managed {
		up := st.pid > 0 && processAlive(st.pid)
		li := LiveInstance{
			Name:     name,
			PID:      st.pid,
			Up:       up,
			MgmtPath: st.desired.MgmtPath,
			ConfPath: st.desired.ConfPath,
			Binary:   st.desired.BinaryPath,
		}
		if up {
			if mgmt, err := b.connectMgmt(st.desired.MgmtPath); err == nil {
				if st2, err := mgmt.Status(ctx); err == nil {
					li.RxBytes = st2.RxBytes
					li.TxBytes = st2.TxBytes
					li.Clients = st2.Clients
				}
				_ = mgmt.Close()
			}
		}
		out = append(out, li)
	}
	return out, nil
}

func (b *HostBackend) EnsureInstance(ctx context.Context, d DesiredInstance) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(d.ConfPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.PIDPath), 0o755); err != nil {
		return err
	}
	if d.CCDDir != "" {
		if err := os.MkdirAll(d.CCDDir, 0o755); err != nil {
			return err
		}
		// write CCD files; remove stale
		existing, _ := os.ReadDir(d.CCDDir)
		keep := map[string]struct{}{}
		for name, content := range d.CCDFiles {
			keep[name] = struct{}{}
			path := filepath.Join(d.CCDDir, name)
			if err := writeFile0600(path, content); err != nil {
				return err
			}
		}
		for _, e := range existing {
			if e.IsDir() {
				continue
			}
			if _, ok := keep[e.Name()]; !ok {
				_ = os.Remove(filepath.Join(d.CCDDir, e.Name()))
			}
		}
	}

	prevContent, _ := os.ReadFile(d.ConfPath)
	if string(prevContent) != d.ConfContent {
		if err := writeFile0600(d.ConfPath, d.ConfContent); err != nil {
			return fmt.Errorf("write conf: %w", err)
		}
	}

	st, ok := b.managed[d.Name]
	if !d.Enabled {
		if ok {
			_ = b.stopLocked(ctx, st)
			st.desired = d
		}
		return nil
	}

	if d.BinaryPath == "" {
		return fmt.Errorf("binary path empty for instance %s", d.Name)
	}

	needStart := !ok || st.pid == 0 || !processAlive(st.pid)
	needRestart := ok && (st.desired.ConfHash != d.ConfHash || st.desired.BinaryPath != d.BinaryPath)

	if needRestart && ok && st.pid > 0 {
		if d.AllowHooks && d.PreDown != "" {
			_ = runHook(ctx, d.PreDown)
		}
		_ = b.stopLocked(ctx, st)
		if d.AllowHooks && d.PostDown != "" {
			_ = runHook(ctx, d.PostDown)
		}
		needStart = true
	}

	if st == nil {
		st = &procState{}
		b.managed[d.Name] = st
	}
	st.desired = d

	if needStart {
		if d.AllowHooks && d.PreUp != "" {
			if err := runHook(ctx, d.PreUp); err != nil {
				return fmt.Errorf("pre_up: %w", err)
			}
		}
		// ensure runtime tmp (openvpn tmp-dir) and remove stale mgmt socket
		if d.PIDPath != "" {
			_ = os.MkdirAll(filepath.Join(filepath.Dir(d.PIDPath), "tmp"), 0o755)
		}
		_ = os.Remove(d.MgmtPath)
		cmd := exec.CommandContext(ctx, d.BinaryPath, "--config", d.ConfPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if len(d.Env) > 0 {
			cmd.Env = append(os.Environ(), d.Env...)
		}
		// capture logs to runtime dir
		logPath := filepath.Join(filepath.Dir(d.PIDPath), d.Name+".log")
		logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		cmd.Stdout = logF
		cmd.Stderr = logF
		if err := cmd.Start(); err != nil {
			_ = logF.Close()
			return fmt.Errorf("start openvpn: %w", err)
		}
		st.cmd = cmd
		st.pid = cmd.Process.Pid
		// wait briefly for pid file / process settle
		go func() {
			_ = cmd.Wait()
			_ = logF.Close()
			b.mu.Lock()
			if cur, ok := b.managed[d.Name]; ok && cur.cmd == cmd {
				cur.pid = 0
				cur.cmd = nil
			}
			b.mu.Unlock()
		}()
		// prefer pid file if written; wait for management socket (up to ~3s)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if pid := readPIDFile(d.PIDPath); pid > 0 {
				st.pid = pid
			}
			if d.MgmtPath != "" {
				if _, err := os.Stat(d.MgmtPath); err == nil {
					break
				}
			}
			// process died early — surface last log lines
			if !processAlive(st.pid) {
				tail := tailFile(logPath, 8)
				if tail != "" {
					return fmt.Errorf("openvpn exited during start:\n%s", tail)
				}
				return fmt.Errorf("openvpn exited during start (see %s)", logPath)
			}
			time.Sleep(100 * time.Millisecond)
		}
		if d.AllowHooks && d.PostUp != "" {
			_ = runHook(ctx, d.PostUp)
		}
	}
	return nil
}

func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (b *HostBackend) StopInstance(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.managed[name]
	if !ok {
		// try pid file
		pidPath := filepath.Join(b.opts.RuntimeDir, name+".pid")
		if pid := readPIDFile(pidPath); pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		return nil
	}
	return b.stopLocked(ctx, st)
}

func (b *HostBackend) stopLocked(ctx context.Context, st *procState) error {
	_ = ctx
	pid := st.pid
	if pid <= 0 {
		pid = readPIDFile(st.desired.PIDPath)
	}
	if pid > 0 && processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !processAlive(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	st.pid = 0
	st.cmd = nil
	if st.desired.MgmtPath != "" {
		_ = os.Remove(st.desired.MgmtPath)
	}
	if st.desired.PIDPath != "" {
		_ = os.Remove(st.desired.PIDPath)
	}
	return nil
}

func (b *HostBackend) RemoveInstance(ctx context.Context, name string) error {
	if err := b.StopInstance(ctx, name); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.managed, name)
	// leave conf in place for hybrid persistence; runtime cleaned by stop
	return nil
}

func (b *HostBackend) Management(ctx context.Context, name string) (MgmtClient, error) {
	_ = ctx
	b.mu.Lock()
	st, ok := b.managed[name]
	path := ""
	if ok {
		path = st.desired.MgmtPath
	}
	b.mu.Unlock()
	if path == "" {
		path = filepath.Join(b.opts.RuntimeDir, name+".mgmt.sock")
	}
	return b.connectMgmt(path)
}

func (b *HostBackend) connectMgmt(path string) (MgmtClient, error) {
	return dialMgmt(path)
}

func (b *HostBackend) WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFile0600(path, content)
}

func (b *HostBackend) RunHook(ctx context.Context, hook string) error {
	if !b.opts.AllowHooks {
		return fmt.Errorf("hooks disabled")
	}
	return runHook(ctx, hook)
}

func (b *HostBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, st := range b.managed {
		_ = b.stopLocked(context.Background(), st)
	}
	return nil
}

func writeFile0600(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func runHook(ctx context.Context, hook string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", hook)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook failed: %w: %s", err, buf.String())
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func readPIDFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return parsePIDFileContent(b)
}

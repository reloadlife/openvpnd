package ovpnbackend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if !ok {
		st = &procState{}
		b.managed[d.Name] = st
		ok = true
	}
	if !d.Enabled {
		_ = b.stopLocked(ctx, st)
		// also reap any orphan still bound to this conf
		_ = b.killOrphansForConf(d.ConfPath, d.PIDPath)
		st.desired = d
		return nil
	}

	if d.BinaryPath == "" {
		return fmt.Errorf("binary path empty for instance %s", d.Name)
	}

	// Refresh tracking from pid file / conf match if our handle went stale.
	if st.pid > 0 && !processAlive(st.pid) {
		st.pid = 0
		st.cmd = nil
	}
	if st.pid == 0 {
		if pid := b.findRunningPID(d); pid > 0 {
			st.pid = pid
		}
	}

	needRestart := st.desired.ConfHash != "" && (st.desired.ConfHash != d.ConfHash || st.desired.BinaryPath != d.BinaryPath)
	needStart := st.pid == 0 || !processAlive(st.pid)

	// If process is up and mgmt answers, keep it (unless conf/binary changed).
	if !needStart && !needRestart && d.MgmtPath != "" {
		if mgmt, err := dialMgmt(d.MgmtPath); err == nil {
			_ = mgmt.Close()
			st.desired = d
			return nil
		}
		// Process up but mgmt path orphaned (classic thrash side-effect). Restart cleanly.
		needRestart = true
	}

	if needRestart && st.pid > 0 && processAlive(st.pid) {
		if d.AllowHooks && d.PreDown != "" {
			_ = runHook(ctx, d.PreDown)
		}
		_ = b.stopLocked(ctx, st)
		if d.AllowHooks && d.PostDown != "" {
			_ = runHook(ctx, d.PostDown)
		}
		needStart = true
	}

	st.desired = d

	if needStart {
		if d.AllowHooks && d.PreUp != "" {
			if err := runHook(ctx, d.PreUp); err != nil {
				return fmt.Errorf("pre_up: %w", err)
			}
		}
		// Ensure no leftover process still holds port/mgmt before we bind a new one.
		_ = b.killOrphansForConf(d.ConfPath, d.PIDPath)
		if d.PIDPath != "" {
			_ = os.MkdirAll(filepath.Join(filepath.Dir(d.PIDPath), "tmp"), 0o755)
			_ = os.Remove(d.PIDPath)
		}
		if d.MgmtPath != "" {
			_ = os.Remove(d.MgmtPath)
		}

		// Long-lived process: do NOT use CommandContext — reconcile ctx must not kill openvpn.
		cmd := exec.Command(d.BinaryPath, "--config", d.ConfPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if len(d.Env) > 0 {
			cmd.Env = append(os.Environ(), d.Env...)
		}
		logPath := filepath.Join(filepath.Dir(d.PIDPath), d.Name+".log")
		if d.PIDPath == "" {
			logPath = filepath.Join(b.opts.RuntimeDir, d.Name+".log")
		}
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
		childPID := cmd.Process.Pid
		st.pid = childPID
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

		// Wait until management is dialable (file existence alone is not enough —
		// thrash can leave a dead sock path while an older process holds the real listen).
		deadline := time.Now().Add(5 * time.Second)
		var lastDial error
		for time.Now().Before(deadline) {
			// Never replace childPID with a stale pid-file value from a previous run.
			if pid := readPIDFile(d.PIDPath); pid > 0 && processAlive(pid) {
				// Prefer pidfile only when it matches our child or child already reaped/replaced.
				if pid == childPID || !processAlive(childPID) {
					st.pid = pid
				}
			}
			if !processAlive(childPID) && (st.pid == 0 || !processAlive(st.pid)) {
				tail := tailFile(logPath, 8)
				if tail != "" {
					return fmt.Errorf("openvpn exited during start:\n%s", tail)
				}
				return fmt.Errorf("openvpn exited during start (see %s)", logPath)
			}
			if d.MgmtPath != "" {
				if mgmt, err := dialMgmt(d.MgmtPath); err == nil {
					_ = mgmt.Close()
					lastDial = nil
					break
				} else {
					lastDial = err
				}
			} else if processAlive(st.pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if d.MgmtPath != "" && lastDial != nil {
			// Process may still be up; surface dial error so reconciler records last_error.
			// Keep tracking pid so we do not thrash-start another copy on the next tick.
			if processAlive(st.pid) || processAlive(childPID) {
				if processAlive(childPID) {
					st.pid = childPID
				}
				return fmt.Errorf("openvpn started (pid %d) but management not ready: %w", st.pid, lastDial)
			}
			tail := tailFile(logPath, 8)
			if tail != "" {
				return fmt.Errorf("openvpn exited during start:\n%s", tail)
			}
			return fmt.Errorf("openvpn exited during start (see %s)", logPath)
		}
		if d.AllowHooks && d.PostUp != "" {
			_ = runHook(ctx, d.PostUp)
		}
	}
	return nil
}

// findRunningPID locates an already-running openvpn for this instance (pidfile or /proc).
func (b *HostBackend) findRunningPID(d DesiredInstance) int {
	if pid := readPIDFile(d.PIDPath); pid > 0 && processAlive(pid) && processMatchesConf(pid, d.ConfPath) {
		return pid
	}
	if pid := findPIDByConf(d.ConfPath); pid > 0 {
		return pid
	}
	return 0
}

// killOrphansForConf stops any openvpn still running with this conf (and pidfile target).
func (b *HostBackend) killOrphansForConf(confPath, pidPath string) error {
	seen := map[int]struct{}{}
	if pid := readPIDFile(pidPath); pid > 0 {
		seen[pid] = struct{}{}
	}
	if pid := findPIDByConf(confPath); pid > 0 {
		seen[pid] = struct{}{}
	}
	for pid := range seen {
		if !processAlive(pid) {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if !processAlive(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			time.Sleep(50 * time.Millisecond)
		}
	}
	return nil
}

// findPIDByConf walks /proc for openvpn --config <confPath>.
func findPIDByConf(confPath string) int {
	if confPath == "" {
		return 0
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		if processMatchesConf(pid, confPath) {
			return pid
		}
	}
	return 0
}

func processMatchesConf(pid int, confPath string) bool {
	if pid <= 0 || confPath == "" {
		return false
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(b) == 0 {
		return false
	}
	// cmdline is NUL-separated
	parts := strings.Split(string(b), "\x00")
	joined := strings.Join(parts, " ")
	if !strings.Contains(joined, confPath) {
		return false
	}
	// require openvpn-ish binary name
	low := strings.ToLower(joined)
	return strings.Contains(low, "openvpn")
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
	pids := map[int]struct{}{}
	if st.pid > 0 {
		pids[st.pid] = struct{}{}
	}
	if pid := readPIDFile(st.desired.PIDPath); pid > 0 {
		pids[pid] = struct{}{}
	}
	if st.desired.ConfPath != "" {
		if pid := findPIDByConf(st.desired.ConfPath); pid > 0 {
			pids[pid] = struct{}{}
		}
	}
	for pid := range pids {
		if pid <= 0 || !processAlive(pid) {
			continue
		}
		// Kill process group when we started with Setpgid.
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		_ = syscall.Kill(pid, syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !processAlive(pid) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
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
	if path == "" {
		return nil, fmt.Errorf("mgmt dial: empty path")
	}
	var last error
	// Brief retry: socket can appear a moment before listen(), or reconciler can race start.
	for i := 0; i < 5; i++ {
		mgmt, err := dialMgmt(path)
		if err == nil {
			return mgmt, nil
		}
		last = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, last
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

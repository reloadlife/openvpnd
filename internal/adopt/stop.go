package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// stopWait is how long StopProcess waits after SIGTERM before SIGKILL.
const stopWait = 5 * time.Second

// test hooks (overridden in unit tests)
var (
	signalProcess = func(pid int, sig syscall.Signal) error {
		return syscall.Kill(pid, sig)
	}
	sleepFn = time.Sleep
	nowFn   = time.Now
)

// ProcessInfo is a snapshot of a /proc entry used for take-over safety checks.
type ProcessInfo struct {
	PID     int
	Argv    []string
	Binary  string
	Cmdline string
	// SafeToStop is true only when the process is a live openvpn dataplane
	// process (not openvpnd/openvpnctl).
	SafeToStop bool
	// Reason explains why SafeToStop is false when applicable.
	Reason string
}

// InspectProcess reads /proc/<pid> and decides whether the process is a safe
// openvpn target for adopt take-over. It never signals.
func InspectProcess(pid int) (ProcessInfo, error) {
	return inspectProcess(procRoot, pid)
}

func inspectProcess(root string, pid int) (ProcessInfo, error) {
	info := ProcessInfo{PID: pid}
	if pid <= 0 {
		info.Reason = "invalid pid"
		return info, fmt.Errorf("invalid pid %d", pid)
	}
	procDir := filepath.Join(root, strconv.Itoa(pid))
	raw, err := os.ReadFile(filepath.Join(procDir, "cmdline"))
	if err != nil {
		if os.IsNotExist(err) {
			info.Reason = "process not found"
			return info, fmt.Errorf("pid %d: process not found", pid)
		}
		info.Reason = "read cmdline failed"
		return info, fmt.Errorf("pid %d: read cmdline: %w", pid, err)
	}
	if len(raw) == 0 {
		info.Reason = "empty cmdline"
		return info, fmt.Errorf("pid %d: empty cmdline", pid)
	}
	argv := SplitCmdline(string(raw))
	info.Argv = argv
	info.Cmdline = strings.Join(argv, " ")

	exePath := ""
	if link, err := os.Readlink(filepath.Join(procDir, "exe")); err == nil {
		exePath = link
	}
	if len(argv) > 0 {
		info.Binary = argv[0]
	}
	if exePath != "" {
		info.Binary = exePath
	}

	// NEVER kill openvpnd / openvpnctl.
	if len(argv) > 0 && isOpenVPNDarg(argv[0]) {
		info.Reason = "refusing openvpnd/openvpnctl process"
		return info, fmt.Errorf("pid %d: refusing to signal openvpnd/openvpnctl", pid)
	}
	if exePath != "" && isOpenVPNDarg(exePath) {
		info.Reason = "refusing openvpnd/openvpnctl process"
		return info, fmt.Errorf("pid %d: refusing to signal openvpnd/openvpnctl", pid)
	}

	isOVPN := IsOpenVPNArgv(argv) || isOpenVPNBinary(exePath)
	if !isOVPN {
		info.Reason = "not an openvpn process"
		return info, fmt.Errorf("pid %d: not an openvpn process (cmdline=%q)", pid, info.Cmdline)
	}

	info.SafeToStop = true
	return info, nil
}

// StopProcess double-checks that pid is a live openvpn process, then sends
// SIGTERM, waits up to 5s, and SIGKILL if still alive. It never signals
// openvpnd or openvpnctl. Soft failures for missing PIDs are returned as errors
// for the caller to record in notes; the caller should still succeed adopt.
func StopProcess(pid int) error {
	return stopProcess(procRoot, pid)
}

func stopProcess(root string, pid int) error {
	// Initial identity check.
	info, err := inspectProcess(root, pid)
	if err != nil {
		return err
	}
	if !info.SafeToStop {
		if info.Reason != "" {
			return fmt.Errorf("pid %d: %s", pid, info.Reason)
		}
		return fmt.Errorf("pid %d: not safe to stop", pid)
	}

	// Re-check cmdline immediately before SIGTERM (TOCTOU harden).
	if err := assertStillOpenVPN(root, pid); err != nil {
		return err
	}
	if err := signalProcess(pid, syscall.SIGTERM); err != nil {
		// Already gone is success-ish for take-over purposes.
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("pid %d: SIGTERM: %w", pid, err)
	}

	deadline := nowFn().Add(stopWait)
	for nowFn().Before(deadline) {
		if !processExists(root, pid) {
			return nil
		}
		// Still alive: confirm it is still the same openvpn (not PID reused).
		if err := assertStillOpenVPN(root, pid); err != nil {
			// Process replaced by something else or gone mid-check — stop signaling.
			if !processExists(root, pid) {
				return nil
			}
			return fmt.Errorf("pid %d: aborted after SIGTERM: %w", pid, err)
		}
		sleepFn(50 * time.Millisecond)
	}

	if !processExists(root, pid) {
		return nil
	}
	// Final identity check before SIGKILL.
	if err := assertStillOpenVPN(root, pid); err != nil {
		return fmt.Errorf("pid %d: refused SIGKILL: %w", pid, err)
	}
	if err := signalProcess(pid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("pid %d: SIGKILL: %w", pid, err)
	}
	// Brief wait for reaping visibility.
	for i := 0; i < 20; i++ {
		if !processExists(root, pid) {
			return nil
		}
		sleepFn(25 * time.Millisecond)
	}
	if processExists(root, pid) {
		return fmt.Errorf("pid %d: still alive after SIGKILL", pid)
	}
	return nil
}

func assertStillOpenVPN(root string, pid int) error {
	info, err := inspectProcess(root, pid)
	if err != nil {
		return err
	}
	if !info.SafeToStop {
		return fmt.Errorf("%s", info.Reason)
	}
	return nil
}

func processExists(root string, pid int) bool {
	if pid <= 0 {
		return false
	}
	// Prefer /proc entry so tests with fake proc work without real signals.
	if root != "/proc" {
		_, err := os.Stat(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
		return err == nil
	}
	err := signalProcess(pid, 0)
	return err == nil
}

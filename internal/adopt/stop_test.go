package adopt

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInspectProcessSafety(t *testing.T) {
	root := t.TempDir()
	old := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = old })

	writeProc(t, root, "1001", "/usr/sbin/openvpn\x00--config\x00/etc/openvpn/s.conf\x00")
	writeProc(t, root, "1002", "/usr/bin/openvpnd\x00run\x00")
	writeProc(t, root, "1003", "/usr/bin/openvpnctl\x00tui\x00")
	writeProc(t, root, "1004", "/bin/bash\x00-c\x00openvpn\x00")
	writeProc(t, root, "1005", "openvpn-linux\x00/opt/x.ovpn\x00")

	info, err := InspectProcess(1001)
	require.NoError(t, err)
	require.True(t, info.SafeToStop)
	require.Contains(t, info.Cmdline, "openvpn")

	_, err = InspectProcess(1002)
	require.Error(t, err)
	require.Contains(t, err.Error(), "openvpnd")

	_, err = InspectProcess(1003)
	require.Error(t, err)
	require.Contains(t, err.Error(), "openvpnctl")

	_, err = InspectProcess(1004)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an openvpn")

	info, err = InspectProcess(1005)
	require.NoError(t, err)
	require.True(t, info.SafeToStop)

	_, err = InspectProcess(0)
	require.Error(t, err)

	_, err = InspectProcess(99999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestStopProcessSIGTERMThenGone(t *testing.T) {
	root := t.TempDir()
	oldRoot := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = oldRoot })

	writeProc(t, root, "4242", "/usr/sbin/openvpn\x00--config\x00/tmp/a.conf\x00")

	var got []syscall.Signal
	oldSig := signalProcess
	oldSleep := sleepFn
	signalProcess = func(pid int, sig syscall.Signal) error {
		require.Equal(t, 4242, pid)
		got = append(got, sig)
		// After SIGTERM, remove proc entry so wait loop exits.
		if sig == syscall.SIGTERM {
			_ = os.RemoveAll(filepath.Join(root, "4242"))
		}
		return nil
	}
	sleepFn = func(d time.Duration) {}
	t.Cleanup(func() {
		signalProcess = oldSig
		sleepFn = oldSleep
	})

	require.NoError(t, StopProcess(4242))
	require.Equal(t, []syscall.Signal{syscall.SIGTERM}, got)
}

func TestStopProcessEscalatesToSIGKILL(t *testing.T) {
	root := t.TempDir()
	oldRoot := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = oldRoot })

	writeProc(t, root, "5151", "/usr/sbin/openvpn\x00--config\x00/tmp/b.conf\x00")

	var got []syscall.Signal
	oldSig := signalProcess
	oldSleep := sleepFn
	oldNow := nowFn
	start := time.Unix(0, 0)
	// Advance "now" past stopWait after a few polls so SIGKILL path runs.
	ticks := 0
	nowFn = func() time.Time {
		// first call for deadline, subsequent for loop checks
		t0 := start.Add(time.Duration(ticks) * time.Second)
		if ticks < 6 {
			ticks++
		}
		return t0
	}
	signalProcess = func(pid int, sig syscall.Signal) error {
		require.Equal(t, 5151, pid)
		got = append(got, sig)
		if sig == syscall.SIGKILL {
			_ = os.RemoveAll(filepath.Join(root, "5151"))
		}
		return nil
	}
	sleepFn = func(d time.Duration) {}
	t.Cleanup(func() {
		signalProcess = oldSig
		sleepFn = oldSleep
		nowFn = oldNow
	})

	require.NoError(t, StopProcess(5151))
	require.Equal(t, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, got)
}

func TestStopProcessRefusesOpenvpnd(t *testing.T) {
	root := t.TempDir()
	oldRoot := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = oldRoot })

	writeProc(t, root, "7", "/usr/bin/openvpnd\x00run\x00")

	signaled := false
	oldSig := signalProcess
	signalProcess = func(pid int, sig syscall.Signal) error {
		signaled = true
		return nil
	}
	t.Cleanup(func() { signalProcess = oldSig })

	err := StopProcess(7)
	require.Error(t, err)
	require.Contains(t, err.Error(), "openvpnd")
	require.False(t, signaled, "must never signal openvpnd")
}

func TestStopProcessRefusesNonOpenVPN(t *testing.T) {
	root := t.TempDir()
	oldRoot := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = oldRoot })

	writeProc(t, root, "9", "/usr/bin/sshd\x00-D\x00")

	signaled := false
	oldSig := signalProcess
	signalProcess = func(pid int, sig syscall.Signal) error {
		signaled = true
		return nil
	}
	t.Cleanup(func() { signalProcess = oldSig })

	err := StopProcess(9)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an openvpn")
	require.False(t, signaled)
}

func TestStopProcessMissingPID(t *testing.T) {
	root := t.TempDir()
	oldRoot := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = oldRoot })

	err := StopProcess(40404)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

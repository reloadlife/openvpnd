package ovpnbackend

import (
	"bufio"
	"encoding/hex"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// commonOpenVPNPaths are fallback locations when openvpn is not on PATH
// (e.g. Debian/Ubuntu install under /usr/sbin, often missing from non-login PATH).
var commonOpenVPNPaths = []string{
	"/usr/sbin/openvpn",
	"/usr/local/sbin/openvpn",
	"/sbin/openvpn",
	"/usr/bin/openvpn",
}

// FindOpenVPN returns the path to an openvpn binary, or "" if none is found.
func FindOpenVPN() string {
	if p, err := exec.LookPath("openvpn"); err == nil && p != "" {
		return p
	}
	for _, p := range commonOpenVPNPaths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// HasNetAdmin reports whether the process is likely able to create TUN/TAP
// devices (root or effective CAP_NET_ADMIN on Linux).
func HasNetAdmin() bool {
	if os.Geteuid() == 0 {
		return true
	}
	return effectiveCap(12) // CAP_NET_ADMIN
}

// firstVersionLine returns the first non-empty line of openvpn --version output.
func firstVersionLine(out string) string {
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.TrimSpace(line)
}

// looksLikeOpenVPNVersion reports whether s resembles openvpn --version output.
func looksLikeOpenVPNVersion(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), "openvpn")
}

// parsePIDFileContent parses a PID file body (digits, optional trailing newline).
// Returns 0 on empty or invalid content.
func parsePIDFileContent(b []byte) int {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return pid
}

// effectiveCap reports whether Linux capability bit n is set in CapEff.
// Returns false on non-Linux or if /proc is unavailable.
func effectiveCap(n uint) bool {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		h := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		if len(h)%2 == 1 {
			h = "0" + h
		}
		raw, err := hex.DecodeString(h)
		if err != nil {
			return false
		}
		byteIdxFromRight := int(n / 8)
		bitInByte := n % 8
		if byteIdxFromRight >= len(raw) {
			return false
		}
		b := raw[len(raw)-1-byteIdxFromRight]
		return b&(1<<bitInByte) != 0
	}
	return false
}

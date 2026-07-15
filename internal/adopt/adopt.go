// Package adopt discovers running OpenVPN processes and maps on-disk conf
// files into openvpnd instance create requests.
package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/reloadlife/openvpnd/internal/confimport"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

// Candidate is a running OpenVPN process discovered on the host.
type Candidate struct {
	PID      int    `json:"pid"`
	ConfPath string `json:"conf_path,omitempty"`
	Cmdline  string `json:"cmdline"`
	Binary   string `json:"binary"`
}

// AdoptResult is a conf file mapped into create fields.
type AdoptResult struct {
	ConfPath string
	Parsed   confimport.Result
	Request  pkgapi.InstanceCreateRequest
	Warnings []string
}

// procRoot is /proc by default; tests may override.
var procRoot = "/proc"

// DiscoverOpenVPN scans the Linux process table for openvpn binaries and
// extracts conf paths from argv when possible.
func DiscoverOpenVPN() ([]Candidate, error) {
	return discoverOpenVPN(procRoot)
}

func discoverOpenVPN(root string) ([]Candidate, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var out []Candidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		argv := SplitCmdline(string(raw))
		if len(argv) == 0 {
			continue
		}
		if !IsOpenVPNArgv(argv) {
			continue
		}
		binary := argv[0]
		confPath := ConfigPathFromArgv(argv)
		// Resolve relative conf against process cwd when possible.
		if confPath != "" && !filepath.IsAbs(confPath) {
			if cwd, err := os.Readlink(filepath.Join(root, e.Name(), "cwd")); err == nil && cwd != "" {
				confPath = filepath.Join(cwd, confPath)
			}
		}
		out = append(out, Candidate{
			PID:      pid,
			ConfPath: confPath,
			Cmdline:  strings.Join(argv, " "),
			Binary:   binary,
		})
	}
	return out, nil
}

// SplitCmdline splits a /proc/<pid>/cmdline buffer (NUL-separated args,
// optionally trailing NUL) into argv. If no NULs are present, falls back to
// shell-ish field splitting for test strings.
func SplitCmdline(raw string) []string {
	if raw == "" {
		return nil
	}
	// Trim trailing NULs common in /proc.
	raw = strings.TrimRight(raw, "\x00")
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "\x00") {
		parts := strings.Split(raw, "\x00")
		var out []string
		for _, p := range parts {
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	// Fake cmdline for unit tests: space-separated, with simple quotes.
	return splitFieldsSimple(raw)
}

// IsOpenVPNArgv reports whether argv looks like an openvpn process.
func IsOpenVPNArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	return isOpenVPNBinary(argv[0])
}

func isOpenVPNBinary(argv0 string) bool {
	base := filepath.Base(argv0)
	// strip version suffixes like openvpn-2.6
	base = strings.ToLower(base)
	if base == "openvpn" {
		return true
	}
	if strings.HasPrefix(base, "openvpn-") || strings.HasPrefix(base, "openvpn.") {
		return true
	}
	// some installs: /usr/local/sbin/openvpn26
	if strings.Contains(base, "openvpn") && !strings.Contains(base, "openvpnd") && !strings.Contains(base, "openvpnctl") {
		return true
	}
	return false
}

// ConfigPathFromArgv extracts the conf path from openvpn argv.
// Recognizes --config PATH, --config=PATH, -config PATH, and a bare *.conf arg.
func ConfigPathFromArgv(argv []string) string {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--config" || a == "-config" || a == "--conf" || a == "-conf":
			if i+1 < len(argv) {
				return argv[i+1]
			}
		case strings.HasPrefix(a, "--config="):
			return strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			return strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--conf="):
			return strings.TrimPrefix(a, "--conf=")
		}
	}
	// Bare path ending in .conf / .ovpn (common with openvpn@unit wrappers).
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, "-") {
			continue
		}
		low := strings.ToLower(a)
		if strings.HasSuffix(low, ".conf") || strings.HasSuffix(low, ".ovpn") {
			return a
		}
	}
	return ""
}

// AdoptFromConf reads an OpenVPN conf from disk and maps it to a create request.
// name is optional; empty leaves name for Prepare auto-fill.
func AdoptFromConf(path string, name string) (AdoptResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return AdoptResult{}, fmt.Errorf("conf path required")
	}
	if !filepath.IsAbs(path) {
		return AdoptResult{}, fmt.Errorf("conf_path must be absolute: %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return AdoptResult{}, fmt.Errorf("read conf: %w", err)
	}
	parsed, err := confimport.Parse(string(data))
	if err != nil {
		return AdoptResult{}, fmt.Errorf("parse conf: %w", err)
	}
	req := parsed.ToCreateRequest()
	if strings.TrimSpace(name) != "" {
		req.Name = strings.TrimSpace(name)
	}
	// Operator breadcrumb.
	note := "# adopted from " + path + "\n"
	req.ExtraDirectives = note + req.ExtraDirectives

	return AdoptResult{
		ConfPath: path,
		Parsed:   parsed,
		Request:  req,
		Warnings: append([]string(nil), parsed.Warnings...),
	}, nil
}

func splitFieldsSimple(line string) []string {
	var out []string
	var b strings.Builder
	inQ := false
	var q byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inQ {
			if c == q {
				inQ = false
				continue
			}
			b.WriteByte(c)
			continue
		}
		if c == '"' || c == '\'' {
			inQ = true
			q = c
			continue
		}
		if c == ' ' || c == '\t' {
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteByte(c)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

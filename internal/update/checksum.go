package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// ParseSHA256SUMS parses a GNU-style SHA256SUMS file into basename → hex digest.
// Lines look like: "<hex>  <filename>" or "<hex> *<filename>".
func ParseSHA256SUMS(data []byte) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// dual space or space-star separator
		var hash, name string
		if i := strings.Index(line, "  "); i >= 0 {
			hash = strings.TrimSpace(line[:i])
			name = strings.TrimSpace(line[i+2:])
		} else if i := strings.Index(line, " *"); i >= 0 {
			hash = strings.TrimSpace(line[:i])
			name = strings.TrimSpace(line[i+2:])
		} else {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			hash, name = fields[0], fields[len(fields)-1]
		}
		name = strings.TrimPrefix(name, "*")
		if len(hash) != 64 {
			continue
		}
		base := filepath.Base(name)
		out[base] = strings.ToLower(hash)
		// also index full relative path basename already; keep original key if useful
		if base != name {
			out[name] = strings.ToLower(hash)
		}
	}
	return out
}

// SHA256Hex returns the lowercase hex SHA-256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// VerifySHA256 checks data against expected lowercase or mixed-case hex digest.
func VerifySHA256(data []byte, expectedHex string) error {
	expectedHex = strings.ToLower(strings.TrimSpace(expectedHex))
	if expectedHex == "" {
		return fmt.Errorf("empty expected checksum")
	}
	got := SHA256Hex(data)
	if got != expectedHex {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, expectedHex)
	}
	return nil
}

// LookupChecksum finds a digest for name by exact basename or any map key ending with name.
func LookupChecksum(sums map[string]string, name string) (string, bool) {
	if sums == nil {
		return "", false
	}
	base := filepath.Base(name)
	if h, ok := sums[base]; ok {
		return h, true
	}
	if h, ok := sums[name]; ok {
		return h, true
	}
	for k, h := range sums {
		if filepath.Base(k) == base {
			return h, true
		}
	}
	return "", false
}

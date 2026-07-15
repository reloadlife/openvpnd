package update

import (
	"strconv"
	"strings"
)

// NormalizeVersion strips a leading "v" and any "+metadata" / "-dirty" noise
// for comparison. Empty and non-semver strings (e.g. "dev") stay as-is.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	// git describe --dirty appends -dirty; drop build metadata after +
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return v
}

// IsDev reports whether v is a development / unreleased build marker.
func IsDev(v string) bool {
	n := strings.ToLower(NormalizeVersion(v))
	return n == "" || n == "dev" || n == "none" || n == "unknown" || strings.HasPrefix(n, "dev-")
}

// CompareVersions returns -1 if a < b, 0 if equal, 1 if a > b.
// Prefers numeric major.minor.patch; falls back to string compare.
// Dev markers compare as older than any release tag.
func CompareVersions(a, b string) int {
	if IsDev(a) && IsDev(b) {
		return 0
	}
	if IsDev(a) {
		return -1
	}
	if IsDev(b) {
		return 1
	}
	na, nb := NormalizeVersion(a), NormalizeVersion(b)
	pa, oka := parseSemver(na)
	pb, okb := parseSemver(nb)
	if oka && okb {
		for i := 0; i < 3; i++ {
			if pa[i] < pb[i] {
				return -1
			}
			if pa[i] > pb[i] {
				return 1
			}
		}
		// pre-release suffix: 1.0.0-rc1 < 1.0.0
		sa, sb := semverSuffix(na), semverSuffix(nb)
		if sa == sb {
			return 0
		}
		if sa == "" {
			return 1
		}
		if sb == "" {
			return -1
		}
		return strings.Compare(sa, sb)
	}
	return strings.Compare(na, nb)
}

// IsNewer reports whether candidate is strictly newer than current.
func IsNewer(candidate, current string) bool {
	return CompareVersions(candidate, current) > 0
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	// strip pre-release for numeric part
	core := v
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		if p == "" {
			return out, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func semverSuffix(v string) string {
	if i := strings.IndexByte(v, '-'); i >= 0 {
		return v[i+1:]
	}
	return ""
}

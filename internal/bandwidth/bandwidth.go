// Package bandwidth plans and applies traffic shaping and checks cumulative
// traffic limits for openvpnd.
//
// Role semantics (keep separate — do not mix):
//
//   - server: per-peer limits on Client rows (CN + static_ip → tc filters),
//     optional instance-level Device ceiling via PlanDevice.
//   - client: whole-tunnel limits on the instance Device (PlanDevice only);
//     there are no VPN "peers" to shape individually.
package bandwidth

import (
	"fmt"
	"net"
	"strings"
)

// Mode selects how bandwidth limits are enforced on the host.
type Mode string

const (
	// ModeOff disables shaping (traffic-limit suspend still handled by reconciler).
	ModeOff Mode = "off"
	// ModeTC applies Linux tc HTB + ingress police per client static IP.
	ModeTC Mode = "tc"
	// ModeShaper uses OpenVPN global --shaper only (confgen); no per-client tc.
	ModeShaper Mode = "shaper"
	// ModeLog plans rules and logs them without executing.
	ModeLog Mode = "log"
)

// NormalizeMode parses config text into a Mode (default off).
func NormalizeMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(ModeTC), "htb":
		return ModeTC
	case string(ModeShaper):
		return ModeShaper
	case string(ModeLog):
		return ModeLog
	case string(ModeOff), "", "none", "false":
		return ModeOff
	default:
		return ModeOff
	}
}

// ClientLimit is the shaping input for one client.
type ClientLimit struct {
	CommonName string
	StaticIP   string
	RxBps      int64 // server→client (egress), bits/sec
	TxBps      int64 // client→server (ingress police), bits/sec
}

// Rule is one host command to apply or reverse shaping.
type Rule struct {
	// Bin is the executable name (e.g. "tc"). Empty means log-only.
	Bin string
	// Args are argv after the binary.
	Args []string
	// Desc is a human-readable summary for logs.
	Desc string
	// Undo is true when this rule removes state (used on Remove).
	Undo bool
}

// String returns a shell-like representation.
func (r Rule) String() string {
	if r.Bin == "" {
		return r.Desc
	}
	return r.Bin + " " + strings.Join(r.Args, " ")
}

// PlanInput is the full input for generating tc rules for one client.
type PlanInput struct {
	Device   string // TUN/TAP interface (required for tc)
	StaticIP string
	RxBps    int64  // download limit for client (bits/sec), 0 = unlimited
	TxBps    int64  // upload limit for client (bits/sec), 0 = unlimited
	ClassID  uint32 // HTB minor class id (2..0xfffe); 0 → 10
}

// Plan returns tc HTB (egress) and ingress police rules for a client static IP.
// Rules are ordered for Apply; callers reverse Undo rules for Remove.
// Bandwidth fields are bits per second (consistent with openvpnd rate metrics).
func Plan(in PlanInput) []Rule {
	dev := strings.TrimSpace(in.Device)
	ip := strings.TrimSpace(in.StaticIP)
	if dev == "" || ip == "" {
		return nil
	}
	if net.ParseIP(ip) == nil {
		return nil
	}
	if in.RxBps <= 0 && in.TxBps <= 0 {
		return nil
	}
	classID := in.ClassID
	if classID == 0 {
		classID = 10
	}
	if classID < 2 {
		classID = 2
	}

	var rules []Rule

	// Root HTB + parent class (idempotent replace).
	rules = append(rules, Rule{
		Bin:  "tc",
		Args: []string{"qdisc", "replace", "dev", dev, "root", "handle", "1:", "htb", "default", "999"},
		Desc: fmt.Sprintf("root htb on %s", dev),
	})
	rules = append(rules, Rule{
		Bin:  "tc",
		Args: []string{"class", "replace", "dev", dev, "parent", "1:", "classid", "1:1", "htb", "rate", "10gbit"},
		Desc: fmt.Sprintf("parent class on %s", dev),
	})

	classMinor := fmt.Sprintf("%d", classID)
	flowid := "1:" + classMinor

	if in.RxBps > 0 {
		rate := formatBitRate(in.RxBps)
		// Egress: server → client (client download / RX from client POV).
		rules = append(rules, Rule{
			Bin: "tc",
			Args: []string{
				"class", "replace", "dev", dev, "parent", "1:1",
				"classid", flowid, "htb", "rate", rate, "ceil", rate,
			},
			Desc: fmt.Sprintf("egress class %s rate %s for %s", flowid, rate, ip),
		})
		// Replace filter: delete-by-pref then add is fragile; use u32 with fixed pref = classID.
		pref := fmt.Sprintf("%d", classID)
		rules = append(rules, Rule{
			Bin: "tc",
			Args: []string{
				"filter", "replace", "dev", dev, "protocol", "ip", "parent", "1:",
				"prio", pref, "u32", "match", "ip", "dst", ip + "/32", "flowid", flowid,
			},
			Desc: fmt.Sprintf("egress filter dst %s → %s", ip, flowid),
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"filter", "del", "dev", dev, "protocol", "ip", "parent", "1:", "prio", pref},
			Desc: fmt.Sprintf("undo egress filter prio %s on %s", pref, dev),
			Undo: true,
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"class", "del", "dev", dev, "classid", flowid},
			Desc: fmt.Sprintf("undo egress class %s on %s", flowid, dev),
			Undo: true,
		})
	}

	if in.TxBps > 0 {
		rate := formatBitRate(in.TxBps)
		// Ingress police: client → server (client upload / TX from client POV).
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"qdisc", "replace", "dev", dev, "handle", "ffff:", "ingress"},
			Desc: fmt.Sprintf("ingress qdisc on %s", dev),
		})
		pref := fmt.Sprintf("%d", 10000+classID)
		burst := ingressBurst(in.TxBps)
		rules = append(rules, Rule{
			Bin: "tc",
			Args: []string{
				"filter", "replace", "dev", dev, "parent", "ffff:", "protocol", "ip",
				"prio", pref, "u32", "match", "ip", "src", ip + "/32",
				"police", "rate", rate, "burst", burst, "drop", "flowid", ":1",
			},
			Desc: fmt.Sprintf("ingress police src %s rate %s", ip, rate),
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"filter", "del", "dev", dev, "parent", "ffff:", "prio", pref},
			Desc: fmt.Sprintf("undo ingress filter prio %s on %s", pref, dev),
			Undo: true,
		})
	}

	return rules
}

// ApplyRules returns only non-undo rules (forward direction).
func ApplyRules(rules []Rule) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if !r.Undo {
			out = append(out, r)
		}
	}
	return out
}

// RemoveRules returns undo rules in reverse order.
func RemoveRules(rules []Rule) []Rule {
	var undos []Rule
	for _, r := range rules {
		if r.Undo {
			undos = append(undos, r)
		}
	}
	for i, j := 0, len(undos)-1; i < j; i, j = i+1, j-1 {
		undos[i], undos[j] = undos[j], undos[i]
	}
	return undos
}

// MaxShaperBytesPerSec returns OpenVPN --shaper argument (bytes/sec) from client limits.
// OpenVPN shaper is global outgoing; we take max of rx/tx bitrates across clients / 8.
func MaxShaperBytesPerSec(clients []ClientLimit) int64 {
	var maxBits int64
	for _, c := range clients {
		if c.RxBps > maxBits {
			maxBits = c.RxBps
		}
		if c.TxBps > maxBits {
			maxBits = c.TxBps
		}
	}
	if maxBits <= 0 {
		return 0
	}
	bps := maxBits / 8
	if bps < 1 {
		bps = 1
	}
	return bps
}

// NeedsShaping reports whether the client has a static IP and a positive limit.
func NeedsShaping(staticIP string, rxBps, txBps int64) bool {
	return strings.TrimSpace(staticIP) != "" && (rxBps > 0 || txBps > 0)
}

// NeedsDeviceShaping reports whether a whole-interface (client tunnel / server ceiling) limit is set.
func NeedsDeviceShaping(rxBps, txBps int64) bool {
	return rxBps > 0 || txBps > 0
}

// DevicePlanInput is whole-interface shaping (no per-IP filter).
// Directions match host netdev counters on the TUN:
//
//	RxBps — download (ingress into the host from the tunnel)
//	TxBps — upload (egress from the host into the tunnel)
type DevicePlanInput struct {
	Device string
	RxBps  int64
	TxBps  int64
}

// PlanDevice returns tc rules that rate-limit the entire Device (client-role tunnels).
func PlanDevice(in DevicePlanInput) []Rule {
	dev := strings.TrimSpace(in.Device)
	if dev == "" || !NeedsDeviceShaping(in.RxBps, in.TxBps) {
		return nil
	}
	var rules []Rule

	if in.TxBps > 0 {
		rate := formatBitRate(in.TxBps)
		// Egress HTB: all traffic leaving the host into the tunnel (upload).
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"qdisc", "replace", "dev", dev, "root", "handle", "1:", "htb", "default", "10"},
			Desc: fmt.Sprintf("device root htb on %s", dev),
		})
		rules = append(rules, Rule{
			Bin: "tc",
			Args: []string{
				"class", "replace", "dev", dev, "parent", "1:", "classid", "1:10",
				"htb", "rate", rate, "ceil", rate,
			},
			Desc: fmt.Sprintf("device egress class rate %s on %s", rate, dev),
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"class", "del", "dev", dev, "classid", "1:10"},
			Desc: fmt.Sprintf("undo device egress class on %s", dev),
			Undo: true,
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"qdisc", "del", "dev", dev, "root"},
			Desc: fmt.Sprintf("undo device root qdisc on %s", dev),
			Undo: true,
		})
	}

	if in.RxBps > 0 {
		rate := formatBitRate(in.RxBps)
		burst := ingressBurst(in.RxBps)
		// Ingress police: all traffic arriving from the tunnel (download).
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"qdisc", "replace", "dev", dev, "handle", "ffff:", "ingress"},
			Desc: fmt.Sprintf("device ingress qdisc on %s", dev),
		})
		rules = append(rules, Rule{
			Bin: "tc",
			Args: []string{
				"filter", "replace", "dev", dev, "parent", "ffff:", "protocol", "ip",
				"prio", "1", "u32", "match", "u32", "0", "0",
				"police", "rate", rate, "burst", burst, "drop", "flowid", ":1",
			},
			Desc: fmt.Sprintf("device ingress police rate %s on %s", rate, dev),
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"filter", "del", "dev", dev, "parent", "ffff:", "prio", "1"},
			Desc: fmt.Sprintf("undo device ingress filter on %s", dev),
			Undo: true,
		})
		rules = append(rules, Rule{
			Bin:  "tc",
			Args: []string{"qdisc", "del", "dev", dev, "handle", "ffff:", "ingress"},
			Desc: fmt.Sprintf("undo device ingress qdisc on %s", dev),
			Undo: true,
		})
	}

	return rules
}

// ShaperBytesPerSec converts bits/sec rate fields to OpenVPN --shaper bytes/sec.
// Uses the max of rx/tx when both set (OpenVPN shaper is single outgoing only).
func ShaperBytesPerSec(rxBps, txBps int64) int64 {
	maxBits := rxBps
	if txBps > maxBits {
		maxBits = txBps
	}
	if maxBits <= 0 {
		return 0
	}
	bps := maxBits / 8
	if bps < 1 {
		bps = 1
	}
	return bps
}

// ExceedsTrafficLimit reports whether effective rx+tx reached the quota.
func ExceedsTrafficLimit(effectiveRx, effectiveTx, limitBytes int64) bool {
	if limitBytes <= 0 {
		return false
	}
	if effectiveRx < 0 {
		effectiveRx = 0
	}
	if effectiveTx < 0 {
		effectiveTx = 0
	}
	return effectiveRx+effectiveTx >= limitBytes
}

func formatBitRate(bitsPerSec int64) string {
	if bitsPerSec <= 0 {
		return "1bit"
	}
	return fmt.Sprintf("%dbit", bitsPerSec)
}

func ingressBurst(bitsPerSec int64) string {
	// ~50ms of traffic, min 2k, max 512k (tc burst is in bytes).
	burst := bitsPerSec / 8 / 20
	if burst < 2048 {
		burst = 2048
	}
	if burst > 512*1024 {
		burst = 512 * 1024
	}
	return fmt.Sprintf("%d", burst)
}

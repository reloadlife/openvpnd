package snmp

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/reloadlife/openvpnd/internal/stats"
)

// Variable is a concrete MIB leaf.
type Variable struct {
	OID   OID
	Value Value
}

// MIB is a sorted, walkable snapshot of scalar + table objects.
type MIB struct {
	vars []Variable
}

// BuildMIB constructs the agent MIB from stats cache + system info.
//
// Tree (default enterprise base 1.3.6.1.4.1.66666.2):
//
//	.1.3.6.1.2.1.1.*                         SNMPv2-MIB system (subset)
//	base.1                                   scalars
//	  base.1.1 ovpnInstanceCount Integer32
//	  base.1.2 ovpnClientCount Integer32
//	  base.1.3 ovpnAgentUptime TimeTicks
//	base.2.1                                 ovpnInstanceEntry
//	base.3.1                                 ovpnClientEntry
func BuildMIB(base OID, cache *stats.Cache, started time.Time) *MIB {
	if cache == nil {
		cache = stats.NewCache()
	}
	if len(base) == 0 {
		base = parseOID("1.3.6.1.4.1.66666.2")
	}
	var vars []Variable

	sys := parseOID("1.3.6.1.2.1.1")
	host, _ := os.Hostname()
	if host == "" {
		host = "openvpnd"
	}
	uptime := int64(time.Since(started).Seconds() * 100)
	if uptime < 0 {
		uptime = 0
	}
	vars = append(vars,
		leaf(sys.Child(1, 0), octet("openvpnd - OpenVPN management daemon")),
		leaf(sys.Child(2, 0), oidVal(base.String())),
		leaf(sys.Child(3, 0), timeTicks(uptime)),
		leaf(sys.Child(4, 0), octet("")),
		leaf(sys.Child(5, 0), octet(host)),
		leaf(sys.Child(6, 0), octet("")),
		leaf(sys.Child(7, 0), integer(72)),
	)

	instances, clients := cache.SnapshotMaps()
	instNames := make([]string, 0, len(instances))
	for n := range instances {
		instNames = append(instNames, n)
	}
	sort.Strings(instNames)
	clientKeys := make([]string, 0, len(clients))
	for k := range clients {
		clientKeys = append(clientKeys, k)
	}
	sort.Strings(clientKeys)

	vars = append(vars,
		leaf(base.Child(1, 1, 0), integer(int64(len(instNames)))),
		leaf(base.Child(1, 2, 0), integer(int64(len(clientKeys)))),
		leaf(base.Child(1, 3, 0), timeTicks(uptime)),
	)

	// Instance table: base.2.1.col.row
	// 1 index, 2 name, 3 role, 4 up, 5 port, 6 connectedClients,
	// 7 rxBytes, 8 txBytes, 9 rxBps, 10 txBps, 11 pid
	for row, name := range instNames {
		idx := uint(row + 1)
		st := instances[name]
		up := int64(0)
		if st.Up {
			up = 1
		}
		entry := base.Child(2, 1)
		vars = append(vars,
			leaf(entry.Child(1, idx), integer(int64(idx))),
			leaf(entry.Child(2, idx), octet(name)),
			leaf(entry.Child(3, idx), octet(st.Role)),
			leaf(entry.Child(4, idx), integer(up)),
			leaf(entry.Child(5, idx), integer(int64(st.Port))),
			leaf(entry.Child(6, idx), integer(int64(st.ConnectedClients))),
			leaf(entry.Child(7, idx), counter64(uint64(max0(st.RxBytes)))),
			leaf(entry.Child(8, idx), counter64(uint64(max0(st.TxBytes)))),
			leaf(entry.Child(9, idx), gauge32(uint32(clampU32(st.RxBps)))),
			leaf(entry.Child(10, idx), gauge32(uint32(clampU32(st.TxBps)))),
			leaf(entry.Child(11, idx), integer(int64(st.PID))),
		)
	}

	// Client table: base.3.1.col.row
	// 1 index, 2 instance, 3 commonName, 4 name, 5 realAddress, 6 virtualAddress,
	// 7 connected, 8 suspended, 9 connectedSince, 10 rxBytes, 11 txBytes, 12 rxBps, 13 txBps
	for row, k := range clientKeys {
		idx := uint(row + 1)
		p := clients[k]
		conn, susp := int64(0), int64(0)
		if p.Connected {
			conn = 1
		}
		if p.Suspended {
			susp = 1
		}
		cs := int64(0)
		if !p.ConnectedSince.IsZero() {
			cs = p.ConnectedSince.Unix()
		}
		entry := base.Child(3, 1)
		vars = append(vars,
			leaf(entry.Child(1, idx), integer(int64(idx))),
			leaf(entry.Child(2, idx), octet(p.Instance)),
			leaf(entry.Child(3, idx), octet(p.CommonName)),
			leaf(entry.Child(4, idx), octet(p.Name)),
			leaf(entry.Child(5, idx), octet(p.RealAddress)),
			leaf(entry.Child(6, idx), octet(p.VirtualAddress)),
			leaf(entry.Child(7, idx), integer(conn)),
			leaf(entry.Child(8, idx), integer(susp)),
			leaf(entry.Child(9, idx), integer(cs)),
			leaf(entry.Child(10, idx), counter64(uint64(max0(p.RxBytes)))),
			leaf(entry.Child(11, idx), counter64(uint64(max0(p.TxBytes)))),
			leaf(entry.Child(12, idx), gauge32(uint32(clampU32(p.RxBps)))),
			leaf(entry.Child(13, idx), gauge32(uint32(clampU32(p.TxBps)))),
		)
	}

	sort.Slice(vars, func(i, j int) bool {
		return vars[i].OID.Compare(vars[j].OID) < 0
	})
	return &MIB{vars: vars}
}

func leaf(oid OID, v Value) Variable {
	return Variable{OID: oid, Value: v}
}

func integer(n int64) Value    { return Value{Type: tagInteger, Int: n} }
func octet(s string) Value     { return Value{Type: tagOctetString, Str: s} }
func oidVal(s string) Value    { return Value{Type: tagOID, Str: s} }
func counter64(n uint64) Value { return Value{Type: tagCounter64, U64: n, Int: int64(n)} }
func gauge32(n uint32) Value   { return Value{Type: tagGauge32, Int: int64(n)} }
func timeTicks(n int64) Value  { return Value{Type: tagTimeTicks, Int: n} }

func max0(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func clampU32(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > float64(^uint32(0)) {
		return float64(^uint32(0))
	}
	return f
}

// Get exact OID.
func (m *MIB) Get(oid OID) (Variable, bool) {
	i := m.search(oid)
	if i < len(m.vars) && m.vars[i].OID.Equal(oid) {
		return m.vars[i], true
	}
	return Variable{}, false
}

// GetNext returns the first variable with OID > oid.
func (m *MIB) GetNext(oid OID) (Variable, bool) {
	i := m.search(oid)
	if i < len(m.vars) && m.vars[i].OID.Equal(oid) {
		i++
	}
	for i < len(m.vars) && m.vars[i].OID.Compare(oid) <= 0 {
		i++
	}
	if i >= len(m.vars) {
		return Variable{}, false
	}
	return m.vars[i], true
}

func (m *MIB) search(oid OID) int {
	return sort.Search(len(m.vars), func(i int) bool {
		return m.vars[i].OID.Compare(oid) >= 0
	})
}

// Len returns number of leaves.
func (m *MIB) Len() int { return len(m.vars) }

func (m *MIB) String() string {
	return fmt.Sprintf("MIB(%d leaves)", len(m.vars))
}

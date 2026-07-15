package ovpnbackend

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Mock is an in-memory backend for tests/dev.
type Mock struct {
	mu        sync.Mutex
	instances map[string]*mockInst
	binVer    map[string]string
	kills     []string // "instance/cn" kill history
}

type mockInst struct {
	desired DesiredInstance
	live    LiveInstance
}

// NewMock creates a mock backend.
func NewMock() *Mock {
	return &Mock{
		instances: make(map[string]*mockInst),
		binVer:    make(map[string]string),
	}
}

func (m *Mock) ProbeBinary(ctx context.Context, path string) (string, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.binVer[path]; ok {
		return v, nil
	}
	return "OpenVPN 2.6.0 mock", nil
}

func (m *Mock) ListLive(ctx context.Context) ([]LiveInstance, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]LiveInstance, 0, len(m.instances))
	for _, mi := range m.instances {
		if mi.live.Up {
			out = append(out, mi.live)
		}
	}
	return out, nil
}

func (m *Mock) EnsureInstance(ctx context.Context, d DesiredInstance) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if !d.Enabled {
		if mi, ok := m.instances[d.Name]; ok {
			mi.live.Up = false
			mi.live.PID = 0
		}
		return nil
	}
	mi, ok := m.instances[d.Name]
	if !ok {
		mi = &mockInst{}
		m.instances[d.Name] = mi
	}
	needRestart := !ok || !mi.live.Up || mi.desired.ConfHash != d.ConfHash || mi.desired.BinaryPath != d.BinaryPath
	mi.desired = d
	if needRestart {
		mi.live = LiveInstance{
			Name:      d.Name,
			PID:       1000 + len(m.instances),
			Up:        true,
			MgmtPath:  d.MgmtPath,
			ConfPath:  d.ConfPath,
			Binary:    d.BinaryPath,
			UpdatedAt: time.Now().UTC(),
		}
	}
	return nil
}

func (m *Mock) StopInstance(ctx context.Context, name string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if mi, ok := m.instances[name]; ok {
		mi.live.Up = false
		mi.live.PID = 0
	}
	return nil
}

func (m *Mock) RemoveInstance(ctx context.Context, name string) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.instances, name)
	return nil
}

func (m *Mock) Management(ctx context.Context, name string) (MgmtClient, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	mi, ok := m.instances[name]
	if !ok || !mi.live.Up {
		return nil, fmt.Errorf("instance %q not running", name)
	}
	return &mockMgmt{parent: m, name: name, live: mi.live}, nil
}

func (m *Mock) WriteFile(path, content string) error {
	_ = path
	_ = content
	return nil
}

func (m *Mock) RunHook(ctx context.Context, hook string) error {
	_ = ctx
	_ = hook
	return nil
}

func (m *Mock) Close() error { return nil }

// SetBinaryVersion sets probe result for tests.
func (m *Mock) SetBinaryVersion(path, version string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.binVer[path] = version
}

// SetClients injects live clients for an instance.
func (m *Mock) SetClients(name string, clients []LiveClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mi, ok := m.instances[name]; ok {
		mi.live.Clients = clients
		var rx, tx int64
		for _, c := range clients {
			rx += c.RxBytes
			tx += c.TxBytes
		}
		mi.live.RxBytes = rx
		mi.live.TxBytes = tx
	}
}

// Kills returns recorded KillClient targets as "instance/cn".
func (m *Mock) Kills() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.kills))
	copy(out, m.kills)
	return out
}

type mockMgmt struct {
	parent *Mock
	name   string
	live   LiveInstance
}

func (m *mockMgmt) Status(ctx context.Context) (LiveInstance, error) {
	_ = ctx
	if m.parent != nil {
		m.parent.mu.Lock()
		defer m.parent.mu.Unlock()
		if mi, ok := m.parent.instances[m.name]; ok {
			return mi.live, nil
		}
	}
	return m.live, nil
}

func (m *mockMgmt) KillClient(ctx context.Context, cnOrAddr string) error {
	_ = ctx
	if m.parent != nil {
		m.parent.mu.Lock()
		m.parent.kills = append(m.parent.kills, m.name+"/"+cnOrAddr)
		// Drop from live status so subsequent Status omits them.
		if mi, ok := m.parent.instances[m.name]; ok {
			filtered := mi.live.Clients[:0]
			for _, c := range mi.live.Clients {
				if c.CommonName != cnOrAddr && c.RealAddress != cnOrAddr {
					filtered = append(filtered, c)
				}
			}
			mi.live.Clients = filtered
		}
		m.parent.mu.Unlock()
	}
	return nil
}

func (m *mockMgmt) Signal(ctx context.Context, sig string) error {
	_ = ctx
	_ = sig
	return nil
}

func (m *mockMgmt) Raw(ctx context.Context, cmd string) (string, error) {
	_ = ctx
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", fmt.Errorf("empty management command")
	}
	if strings.ContainsAny(cmd, "\r\n") {
		return "", fmt.Errorf("management command must be a single line")
	}
	fields := strings.Fields(cmd)
	verb := strings.ToLower(fields[0])
	switch verb {
	case "status":
		var b strings.Builder
		b.WriteString("TITLE,OpenVPN Mock Status\n")
		b.WriteString("HEADER,CLIENT_LIST,Common Name,Real Address,Virtual Address,Virtual IPv6 Address,Bytes Received,Bytes Sent,Connected Since\n")
		for _, c := range m.live.Clients {
			since := c.ConnectedSince.UTC().Format(time.RFC3339)
			if c.ConnectedSince.IsZero() {
				since = time.Now().UTC().Format(time.RFC3339)
			}
			fmt.Fprintf(&b, "CLIENT_LIST,%s,%s,%s,,%d,%d,%s\n",
				c.CommonName, c.RealAddress, c.VirtualAddress, c.RxBytes, c.TxBytes, since)
		}
		fmt.Fprintf(&b, "GLOBAL_STATS,Max bcast/mcast queue length,0\n")
		return strings.TrimRight(b.String(), "\n"), nil
	case "kill":
		if len(fields) < 2 {
			return "", fmt.Errorf("mgmt: ERROR: need CN or IP:port")
		}
		return "SUCCESS: common name '" + fields[1] + "' found, 1 client(s) killed", nil
	case "signal":
		if len(fields) < 2 {
			return "", fmt.Errorf("mgmt: ERROR: need signal name")
		}
		return "SUCCESS: signal " + fields[1] + " thrown", nil
	case "hold":
		if len(fields) == 1 {
			return "SUCCESS: hold=0", nil
		}
		return "SUCCESS: hold " + strings.Join(fields[1:], " "), nil
	case "log":
		return "SUCCESS: log command accepted", nil
	case "state":
		return fmt.Sprintf("0,CONNECTED,SUCCESS,,%s", m.live.Name), nil
	case "bytecount":
		return "SUCCESS: bytecount interval changed", nil
	case "pid":
		return fmt.Sprintf("SUCCESS: pid=%d", m.live.PID), nil
	case "version":
		return "OpenVPN Version: OpenVPN 2.6.0 mock\nManagement Version: 1", nil
	default:
		return "", fmt.Errorf("mgmt: ERROR: unknown command: %s", verb)
	}
}

func (m *mockMgmt) Close() error { return nil }

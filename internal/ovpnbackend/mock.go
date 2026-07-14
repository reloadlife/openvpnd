package ovpnbackend

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Mock is an in-memory backend for tests/dev.
type Mock struct {
	mu        sync.Mutex
	instances map[string]*mockInst
	binVer    map[string]string
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
	return &mockMgmt{live: mi.live}, nil
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

type mockMgmt struct {
	live LiveInstance
}

func (m *mockMgmt) Status(ctx context.Context) (LiveInstance, error) {
	_ = ctx
	return m.live, nil
}

func (m *mockMgmt) KillClient(ctx context.Context, cnOrAddr string) error {
	_ = ctx
	_ = cnOrAddr
	return nil
}

func (m *mockMgmt) Signal(ctx context.Context, sig string) error {
	_ = ctx
	_ = sig
	return nil
}

func (m *mockMgmt) Close() error { return nil }

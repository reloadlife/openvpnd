package stats

import (
	"sync"
	"time"
)

// Cache holds the latest rates and counters for metrics/API.
type Cache struct {
	mu        sync.RWMutex
	instances map[string]*InstanceStats
	clients   map[string]*ClientStats // key: instance/cn
}

// InstanceStats is aggregated instance stats.
type InstanceStats struct {
	Name             string
	Role             string
	Up               bool
	PID              int
	Port             int
	ConnectedClients int
	RxBytes          int64
	TxBytes          int64
	RxBps            float64
	TxBps            float64
	LastError        string
	UpdatedAt        time.Time
}

// ClientStats is per-client stats.
type ClientStats struct {
	Instance       string
	CommonName     string
	Name           string
	RealAddress    string
	VirtualAddress string
	Connected      bool
	ConnectedSince time.Time
	RxBytes        int64
	TxBytes        int64
	RxBps          float64
	TxBps          float64
	Suspended      bool
	UpdatedAt      time.Time
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{
		instances: make(map[string]*InstanceStats),
		clients:   make(map[string]*ClientStats),
	}
}

func clientKey(instance, cn string) string { return instance + "/" + cn }

// SetInstance stores instance stats.
func (c *Cache) SetInstance(s InstanceStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := s
	c.instances[s.Name] = &cp
}

// SetClient stores client stats.
func (c *Cache) SetClient(s ClientStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := s
	c.clients[clientKey(s.Instance, s.CommonName)] = &cp
}

// GetInstance returns instance stats.
func (c *Cache) GetInstance(name string) (InstanceStats, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.instances[name]
	if !ok {
		return InstanceStats{}, false
	}
	return *s, true
}

// ListInstances returns all instance stats.
func (c *Cache) ListInstances() []InstanceStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]InstanceStats, 0, len(c.instances))
	for _, s := range c.instances {
		out = append(out, *s)
	}
	return out
}

// ListClients returns all client stats.
func (c *Cache) ListClients() []ClientStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ClientStats, 0, len(c.clients))
	for _, s := range c.clients {
		out = append(out, *s)
	}
	return out
}

// Snapshot totals.
func (c *Cache) Snapshot() (rx, tx int64, rxBps, txBps float64, up, total int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total = len(c.instances)
	for _, s := range c.instances {
		rx += s.RxBytes
		tx += s.TxBytes
		rxBps += s.RxBps
		txBps += s.TxBps
		if s.Up {
			up++
		}
	}
	return
}

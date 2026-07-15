package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reloadlife/openvpnd/internal/bandwidth"
	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/policy"
	"github.com/reloadlife/openvpnd/internal/stats"
)

// Config for the reconciler.
type Config struct {
	ConfDir        string
	RuntimeDir     string
	DefaultBinary  string
	SampleInterval time.Duration
	AllowHooks     bool
	// BandwidthEnforcement: off|tc|shaper|log (see config.DaemonConfig).
	BandwidthEnforcement string
}

// MetricsObserver records reconcile timing (optional).
type MetricsObserver interface {
	ObserveReconcile(d time.Duration, err error)
}

// Reconciler applies desired state and samples stats.
type Reconciler struct {
	store   *db.Store
	backend ovpnbackend.Backend
	cache   *stats.Cache
	cfg     Config
	log     *slog.Logger
	metrics MetricsObserver
	bw      *bandwidth.Enforcer

	mu         sync.Mutex
	prevSample map[string]sample // key instance/cn
	lastErr    error
	// lifecycle edge detection for controller webhooks
	prevUp       map[string]bool            // instance name → was up
	prevPeerConn map[string]map[string]bool // instance → cn → connected
}

type sample struct {
	rx, tx int64
	at     time.Time
}

// New creates a reconciler.
func New(store *db.Store, backend ovpnbackend.Backend, cache *stats.Cache, cfg Config, log *slog.Logger) *Reconciler {
	if cfg.DefaultBinary == "" {
		cfg.DefaultBinary = "default"
	}
	if cfg.SampleInterval <= 0 {
		cfg.SampleInterval = 5 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reconciler{
		store:        store,
		backend:      backend,
		cache:        cache,
		cfg:          cfg,
		log:          log,
		bw:           bandwidth.NewEnforcer(cfg.BandwidthEnforcement, log),
		prevSample:   make(map[string]sample),
		prevUp:       make(map[string]bool),
		prevPeerConn: make(map[string]map[string]bool),
	}
}

// BandwidthEnforcer returns the shaping enforcer (tests may swap the runner).
func (r *Reconciler) BandwidthEnforcer() *bandwidth.Enforcer {
	return r.bw
}

// SetMetrics wires optional reconcile metrics.
func (r *Reconciler) SetMetrics(m MetricsObserver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = m
}

// Exclusive runs fn while holding the reconciler lock.
func (r *Reconciler) Exclusive(fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn()
}

// LastError returns the last reconcile error.
func (r *Reconciler) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

// RunOnce performs one full reconcile + sample cycle.
func (r *Reconciler) RunOnce(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := time.Now()
	err := r.run(ctx)
	r.lastErr = err
	if r.metrics != nil {
		r.metrics.ObserveReconcile(time.Since(start), err)
	}
	return err
}

// Loop runs until ctx is done.
func (r *Reconciler) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	_ = r.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.RunOnce(ctx); err != nil {
				r.log.Warn("reconcile error", "err", err)
			}
		}
	}
}

func (r *Reconciler) run(ctx context.Context) error {
	instances, err := r.store.ListInstances(ctx)
	if err != nil {
		return err
	}
	desiredNames := map[string]struct{}{}

	for i := range instances {
		inst := instances[i]
		desiredNames[inst.Name] = struct{}{}
		if err := r.applyInstance(ctx, inst); err != nil {
			r.log.Error("apply instance", "name", inst.Name, "err", err)
			_ = r.store.UpdateInstanceRuntime(ctx, inst.Name, inst.PID, inst.ConfHash, err.Error(),
				inst.LastRxBytes, inst.LastTxBytes, inst.LastRxBps, inst.LastTxBps, inst.ConnectedClients)
			_ = r.store.AddEvent(ctx, "error", "reconcile", inst.Name, "", err.Error(), "{}")
		}
	}

	// Stop unknown managed live instances that are no longer in DB
	live, _ := r.backend.ListLive(ctx)
	for _, li := range live {
		if _, ok := desiredNames[li.Name]; !ok {
			r.log.Info("removing orphan instance", "name", li.Name)
			_ = r.backend.RemoveInstance(ctx, li.Name)
			if r.bw != nil {
				_ = r.bw.ClearInstance(ctx, li.Name)
			}
		}
	}
	return nil
}

func (r *Reconciler) applyInstance(ctx context.Context, inst db.Instance) error {
	binName := inst.BinaryName
	if binName == "" {
		binName = r.cfg.DefaultBinary
	}
	binPath, err := r.store.ResolveBinaryPath(ctx, binName, inst.BinaryPath)
	if err != nil {
		// fall back to path override only
		if inst.BinaryPath != "" {
			binPath = inst.BinaryPath
		} else {
			return fmt.Errorf("resolve binary: %w", err)
		}
	}

	// probe version occasionally
	if ver, err := r.backend.ProbeBinary(ctx, binPath); err == nil && binName != "" {
		_ = r.store.UpdateBinaryVersion(ctx, binName, ver)
	}

	var clients []db.Client
	if inst.Role == "server" {
		clients, err = r.store.ListClientsByInstance(ctx, inst.Name)
		if err != nil {
			return err
		}
		// Apply expire (and traffic from last-known counters) before CCD so disable is written.
		now := time.Now().UTC()
		for i := range clients {
			c := clients[i]
			if ok, reason := policy.ShouldAutoSuspend(c, now); ok {
				if err := r.store.SetClientSuspended(ctx, c.ID, true); err == nil {
					clients[i].Suspended = true
					r.emitAutoSuspend(ctx, inst.Name, c, reason, c.EffectiveRx(), c.EffectiveTx())
				}
			}
		}
	}

	paths := confgen.Paths{
		ConfDir:    r.cfg.ConfDir,
		RuntimeDir: r.cfg.RuntimeDir,
		Name:       inst.Name,
	}
	customPresets, _ := r.store.ListFeaturePresets(ctx)
	rendered, err := confgen.RenderInstanceOpts(inst, paths, clients, confgen.RenderOptions{
		CustomPresets:        customPresets,
		BandwidthEnforcement: r.cfg.BandwidthEnforcement,
	})
	if err != nil {
		return fmt.Errorf("render conf: %w", err)
	}

	ccdFiles := map[string]string{}
	if inst.Role == "server" {
		for _, c := range clients {
			fn := confgen.SafeCNFilename(c.CommonName)
			ccdFiles[fn] = confgen.RenderCCD(c, inst.ServerNetwork)
		}
	}

	// Expanded env for process (presets + instance)
	_, _, envVars := features.Expand(inst.FeatureSets, customPresets, "", nil, inst.EnvVars)
	var env []string
	for _, e := range envVars {
		if e.Name != "" {
			env = append(env, e.Name+"="+e.Value)
		}
	}

	desired := ovpnbackend.DesiredInstance{
		Name:        inst.Name,
		Role:        inst.Role,
		Enabled:     inst.Enabled,
		BinaryPath:  binPath,
		ConfPath:    paths.ConfFile(),
		ConfContent: rendered.Content,
		ConfHash:    rendered.Hash,
		PIDPath:     paths.PIDFile(),
		MgmtPath:    paths.MgmtSock(),
		StatusPath:  paths.StatusFile(),
		CCDDir:      paths.CCDDir(),
		CCDFiles:    ccdFiles,
		Env:         env,
		PreUp:       inst.PreUp,
		PostUp:      inst.PostUp,
		PreDown:     inst.PreDown,
		PostDown:    inst.PostDown,
		AllowHooks:  r.cfg.AllowHooks,
	}

	if err := r.backend.EnsureInstance(ctx, desired); err != nil {
		return err
	}

	// Bandwidth shaping — role-aware (do not mix models):
	//   server → per-peer Client.bandwidth_* (+ optional instance device ceiling)
	//   client → whole-tunnel instance.bandwidth_* on Device (no peer list)
	if r.bw != nil {
		if !inst.Enabled {
			_ = r.bw.ClearInstance(ctx, inst.Name)
		} else {
			switch strings.ToLower(strings.TrimSpace(inst.Role)) {
			case "server":
				var limits []bandwidth.ClientLimit
				for _, c := range clients {
					if c.Suspended {
						continue
					}
					rx, tx := policy.EffectiveBandwidth(c.BandwidthRxBps, c.BandwidthTxBps, c.BandwidthTotalBps)
					if rx <= 0 && tx <= 0 {
						continue
					}
					limits = append(limits, bandwidth.ClientLimit{
						CommonName: c.CommonName,
						StaticIP:   c.StaticIP,
						RxBps:      rx,
						TxBps:      tx,
					})
				}
				if err := r.bw.Sync(ctx, inst.Name, inst.Device, limits); err != nil {
					r.log.Warn("bandwidth peer sync", "instance", inst.Name, "err", err)
				}
				// Optional server TUN ceiling from instance-level fields.
				if err := r.bw.SyncDevice(ctx, inst.Name, inst.Device, inst.BandwidthRxBps, inst.BandwidthTxBps); err != nil {
					r.log.Warn("bandwidth device ceiling", "instance", inst.Name, "err", err)
				}
			case "client":
				// Client tunnels: shape the entire TUN (zur0/de0/…), not "peers".
				if err := r.bw.SyncDevice(ctx, inst.Name, inst.Device, inst.BandwidthRxBps, inst.BandwidthTxBps); err != nil {
					r.log.Warn("bandwidth tunnel sync", "instance", inst.Name, "err", err)
				}
			}
		}
	}

	// Sample live state
	pid := 0
	up := false
	var rx, tx int64
	var rxBps, txBps float64
	nClients := 0
	lastErr := ""

	if inst.Enabled {
		if mgmt, err := r.backend.Management(ctx, inst.Name); err == nil {
			st, err := mgmt.Status(ctx)
			_ = mgmt.Close()
			if err == nil {
				up = true
				rx, tx = st.RxBytes, st.TxBytes
				nClients = len(st.Clients)
				// Client tunnels often report no CLIENT_LIST; prefer iface counters when Device is set.
				if (rx == 0 && tx == 0) && strings.TrimSpace(inst.Device) != "" {
					if irx, itx, ierr := readIfaceBytes(inst.Device); ierr == nil {
						rx, tx = irx, itx
					}
				}
				now := time.Now().UTC()
				key := inst.Name
				if prev, ok := r.prevSample[key]; ok && !prev.at.IsZero() {
					dt := now.Sub(prev.at).Seconds()
					if dt > 0 {
						rxBps = float64(rx-prev.rx) * 8 / dt
						txBps = float64(tx-prev.tx) * 8 / dt
						if rxBps < 0 {
							rxBps = 0
						}
						if txBps < 0 {
							txBps = 0
						}
					}
				}
				r.prevSample[key] = sample{rx: rx, tx: tx, at: now}

				// update per-client runtime
				byCN := map[string]ovpnbackend.LiveClient{}
				for _, lc := range st.Clients {
					byCN[lc.CommonName] = lc
				}
				// peer connect/disconnect edges for controller webhooks
				curConn := make(map[string]bool, len(byCN))
				for cn := range byCN {
					if cn == "" {
						continue
					}
					curConn[cn] = true
				}
				prevConn := r.prevPeerConn[inst.Name]
				if prevConn == nil {
					prevConn = map[string]bool{}
				}
				for cn := range curConn {
					if !prevConn[cn] {
						_ = r.store.AddEvent(ctx, "info", "peer.connected", inst.Name, cn, "peer connected", "{}")
					}
				}
				for cn := range prevConn {
					if !curConn[cn] {
						_ = r.store.AddEvent(ctx, "info", "peer.disconnected", inst.Name, cn, "peer disconnected", "{}")
					}
				}
				r.prevPeerConn[inst.Name] = curConn

				for i := range clients {
					c := clients[i]
					lc, connected := byCN[c.CommonName]
					crx, ctxb := c.LastRxBytes, c.LastTxBytes
					crxBps, ctxBps := 0.0, 0.0
					realAddr, virtAddr, since := "", "", ""
					if connected {
						crx, ctxb = lc.RxBytes, lc.TxBytes
						realAddr, virtAddr = lc.RealAddress, lc.VirtualAddress
						if !lc.ConnectedSince.IsZero() {
							since = lc.ConnectedSince.UTC().Format(time.RFC3339)
						}
						ck := inst.Name + "/" + c.CommonName
						if prev, ok := r.prevSample[ck]; ok && !prev.at.IsZero() {
							dt := now.Sub(prev.at).Seconds()
							if dt > 0 {
								crxBps = float64(crx-prev.rx) * 8 / dt
								ctxBps = float64(ctxb-prev.tx) * 8 / dt
								if crxBps < 0 {
									crxBps = 0
								}
								if ctxBps < 0 {
									ctxBps = 0
								}
							}
						}
						r.prevSample[ck] = sample{rx: crx, tx: ctxb, at: now}
					}

					// Peer policy: traffic quota (live counters) and late expire check.
					if !c.Suspended {
						tmp := c
						tmp.LastRxBytes = crx
						tmp.LastTxBytes = ctxb
						if ok, reason := policy.ShouldAutoSuspend(tmp, now); ok {
							if err := r.store.SetClientSuspended(ctx, c.ID, true); err == nil {
								c.Suspended = true
								clients[i].Suspended = true
								r.emitAutoSuspend(ctx, inst.Name, tmp, reason, tmp.EffectiveRx(), tmp.EffectiveTx())
							}
						}
					}

					if connected && c.Suspended {
						if mgmt2, err := r.backend.Management(ctx, inst.Name); err == nil {
							_ = mgmt2.KillClient(ctx, c.CommonName)
							_ = mgmt2.Close()
						}
					}

					_ = r.store.UpdateClientRuntime(ctx, c.ID, realAddr, virtAddr, since, crx, ctxb, crxBps, ctxBps)
					if r.cache != nil {
						r.cache.SetClient(stats.ClientStats{
							Instance:       inst.Name,
							CommonName:     c.CommonName,
							Name:           c.Name,
							RealAddress:    realAddr,
							VirtualAddress: virtAddr,
							Connected:      connected && !c.Suspended,
							RxBytes:        crx - c.RxBytesOffset,
							TxBytes:        ctxb - c.TxBytesOffset,
							RxBps:          crxBps,
							TxBps:          ctxBps,
							Suspended:      c.Suspended,
							UpdatedAt:      now,
						})
					}
					if connected {
						_ = r.store.InsertSample(ctx, c.ID, crx, ctxb, crxBps, ctxBps)
					}
				}
			} else {
				lastErr = err.Error()
			}
		} else {
			// management not ready yet — check list live + iface counters
			lives, _ := r.backend.ListLive(ctx)
			for _, li := range lives {
				if li.Name == inst.Name && li.Up {
					up = true
					pid = li.PID
					rx, tx = li.RxBytes, li.TxBytes
				}
			}
			if up && rx == 0 && tx == 0 && strings.TrimSpace(inst.Device) != "" {
				if irx, itx, ierr := readIfaceBytes(inst.Device); ierr == nil {
					rx, tx = irx, itx
				}
			}
			if !up {
				lastErr = err.Error()
			}
		}
	}

	// pid from live list
	if lives, err := r.backend.ListLive(ctx); err == nil {
		for _, li := range lives {
			if li.Name == inst.Name {
				pid = li.PID
				if li.Up {
					up = true
				}
			}
		}
	}

	_ = r.store.UpdateInstanceRuntime(ctx, inst.Name, pid, rendered.Hash, lastErr, rx, tx, rxBps, txBps, nClients)
	instUp := up && inst.Enabled
	if r.cache != nil {
		r.cache.SetInstance(stats.InstanceStats{
			Name:             inst.Name,
			Role:             inst.Role,
			Up:               instUp,
			PID:              pid,
			Port:             inst.Port,
			ConnectedClients: nClients,
			RxBytes:          rx,
			TxBytes:          tx,
			RxBps:            rxBps,
			TxBps:            txBps,
			LastError:        lastErr,
			UpdatedAt:        time.Now().UTC(),
		})
	}

	// Lifecycle edges → events (picked up by webhooks via store hook).
	if was, seen := r.prevUp[inst.Name]; seen && was != instUp {
		if instUp {
			_ = r.store.AddEvent(ctx, "info", "instance.up", inst.Name, "", "instance is up", "{}")
		} else {
			_ = r.store.AddEvent(ctx, "warn", "instance.down", inst.Name, "", "instance is down", "{}")
		}
	}
	r.prevUp[inst.Name] = instUp

	// persist conf hash on instance row for next comparison via UpdateInstance is heavy; runtime is enough
	_ = filepath.Separator
	return nil
}

// emitAutoSuspend records peer.suspended / peer.expired and logs.
func (r *Reconciler) emitAutoSuspend(ctx context.Context, instance string, c db.Client, reason string, effRx, effTx int64) {
	var kind, msg, meta string
	switch reason {
	case policy.ReasonExpired:
		kind = "peer.expired"
		msg = "client expired; suspended"
		meta = fmt.Sprintf(`{"reason":%q,"expires_at":%q}`, policy.ReasonExpired, c.ExpiresAt.UTC().Format(time.RFC3339))
	case policy.ReasonTrafficLimit:
		kind = "peer.suspended"
		msg = fmt.Sprintf("traffic limit exceeded (%d bytes); client suspended", c.TrafficLimitBytes)
		meta = fmt.Sprintf(`{"reason":%q,"rx":%d,"tx":%d,"limit":%d}`,
			policy.ReasonTrafficLimit, effRx, effTx, c.TrafficLimitBytes)
	default:
		kind = "peer.suspended"
		msg = "client auto-suspended"
		meta = fmt.Sprintf(`{"reason":%q}`, reason)
	}
	_ = r.store.AddEvent(ctx, "warn", kind, instance, c.CommonName, msg, meta)
	r.log.Warn("auto-suspending client",
		"instance", instance, "cn", c.CommonName, "reason", reason,
		"rx", effRx, "tx", effTx)
}

// readIfaceBytes returns cumulative rx/tx byte counters for a Linux netdev
// (e.g. zur0, de0). Used for client-tunnel throughput when management has no
// CLIENT_LIST. Rejects names with path separators.
func readIfaceBytes(dev string) (rx, tx int64, err error) {
	dev = strings.TrimSpace(dev)
	if dev == "" || strings.Contains(dev, "/") || strings.Contains(dev, "..") {
		return 0, 0, fmt.Errorf("invalid device %q", dev)
	}
	base := filepath.Join("/sys/class/net", dev, "statistics")
	rb, err := os.ReadFile(filepath.Join(base, "rx_bytes"))
	if err != nil {
		return 0, 0, err
	}
	tb, err := os.ReadFile(filepath.Join(base, "tx_bytes"))
	if err != nil {
		return 0, 0, err
	}
	rx, err = strconv.ParseInt(strings.TrimSpace(string(rb)), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	tx, err = strconv.ParseInt(strings.TrimSpace(string(tb)), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return rx, tx, nil
}

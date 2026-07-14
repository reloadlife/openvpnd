package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
	"github.com/reloadlife/openvpnd/internal/ovpnbackend"
	"github.com/reloadlife/openvpnd/internal/stats"
)

// Config for the reconciler.
type Config struct {
	ConfDir        string
	RuntimeDir     string
	DefaultBinary  string
	SampleInterval time.Duration
	AllowHooks     bool
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

	mu         sync.Mutex
	prevSample map[string]sample // key instance/cn
	lastErr    error
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
		store:      store,
		backend:    backend,
		cache:      cache,
		cfg:        cfg,
		log:        log,
		prevSample: make(map[string]sample),
	}
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
	}

	paths := confgen.Paths{
		ConfDir:    r.cfg.ConfDir,
		RuntimeDir: r.cfg.RuntimeDir,
		Name:       inst.Name,
	}
	customPresets, _ := r.store.ListFeaturePresets(ctx)
	rendered, err := confgen.RenderInstanceOpts(inst, paths, clients, confgen.RenderOptions{CustomPresets: customPresets})
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
				for _, c := range clients {
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
						// enforce suspend
						if c.Suspended {
							if mgmt2, err := r.backend.Management(ctx, inst.Name); err == nil {
								_ = mgmt2.KillClient(ctx, c.CommonName)
								_ = mgmt2.Close()
							}
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
			// management not ready yet — check list live
			lives, _ := r.backend.ListLive(ctx)
			for _, li := range lives {
				if li.Name == inst.Name && li.Up {
					up = true
					pid = li.PID
					rx, tx = li.RxBytes, li.TxBytes
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
	if r.cache != nil {
		r.cache.SetInstance(stats.InstanceStats{
			Name:             inst.Name,
			Role:             inst.Role,
			Up:               up && inst.Enabled,
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

	// persist conf hash on instance row for next comparison via UpdateInstance is heavy; runtime is enough
	_ = filepath.Separator
	return nil
}

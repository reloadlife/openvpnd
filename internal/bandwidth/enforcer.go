package bandwidth

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// Runner executes host commands (overridable in tests).
type Runner interface {
	// LookPath finds an executable (like exec.LookPath).
	LookPath(file string) (string, error)
	// Run executes name with args; returns combined error.
	Run(ctx context.Context, name string, args ...string) error
}

type execRunner struct{}

func (execRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// appliedClient is remembered shaping state for one client on an instance.
type appliedClient struct {
	StaticIP string
	RxBps    int64
	TxBps    int64
	ClassID  uint32
	Device   string
	Rules    []Rule
}

// appliedDevice is whole-interface shaping state (client tunnels / server ceiling).
type appliedDevice struct {
	Device string
	RxBps  int64
	TxBps  int64
	Rules  []Rule
}

// Enforcer applies/removes shaping rules according to Mode.
type Enforcer struct {
	mode   Mode
	log    *slog.Logger
	runner Runner

	mu       sync.Mutex
	applied  map[string]map[string]appliedClient // instance → cn → peer state (server role)
	devices  map[string]appliedDevice            // instance → whole-device state (client role / ceiling)
	nextCls  map[string]uint32                   // device → next class id
	missing  map[string]bool                     // binary name → logged missing once
}

// NewEnforcer builds an enforcer. Unknown modes become ModeOff.
func NewEnforcer(mode string, log *slog.Logger) *Enforcer {
	if log == nil {
		log = slog.Default()
	}
	return &Enforcer{
		mode:    NormalizeMode(mode),
		log:     log,
		runner:  execRunner{},
		applied: make(map[string]map[string]appliedClient),
		devices: make(map[string]appliedDevice),
		nextCls: make(map[string]uint32),
		missing: make(map[string]bool),
	}
}

// SetRunner replaces the command runner (tests).
func (e *Enforcer) SetRunner(r Runner) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r == nil {
		e.runner = execRunner{}
		return
	}
	e.runner = r
}

// Mode returns the configured enforcement mode.
func (e *Enforcer) Mode() Mode {
	if e == nil {
		return ModeOff
	}
	return e.mode
}

// Sync makes host shaping match the desired client set for an instance.
// Clients without static IP or with zero limits are not shaped.
// Suspended clients have rules removed.
// ModeOff / ModeShaper: no host commands (shaper is confgen-only).
func (e *Enforcer) Sync(ctx context.Context, instanceName, device string, clients []ClientLimit) error {
	if e == nil {
		return nil
	}
	switch e.mode {
	case ModeOff, ModeShaper:
		return nil
	case ModeTC, ModeLog:
		// continue
	default:
		return nil
	}

	device = strings.TrimSpace(device)
	desired := make(map[string]ClientLimit)
	for _, c := range clients {
		cn := strings.TrimSpace(c.CommonName)
		if cn == "" {
			continue
		}
		if !NeedsShaping(c.StaticIP, c.RxBps, c.TxBps) {
			continue
		}
		desired[cn] = ClientLimit{
			CommonName: cn,
			StaticIP:   strings.TrimSpace(c.StaticIP),
			RxBps:      c.RxBps,
			TxBps:      c.TxBps,
		}
	}

	e.mu.Lock()
	if e.applied[instanceName] == nil {
		e.applied[instanceName] = make(map[string]appliedClient)
	}
	// Snapshot CNs to remove / update outside lock for runner calls? We hold lock
	// only for bookkeeping; runRules is called carefully.
	current := e.applied[instanceName]
	var toRemove []string
	for cn := range current {
		if _, ok := desired[cn]; !ok {
			toRemove = append(toRemove, cn)
		}
	}
	sort.Strings(toRemove)
	e.mu.Unlock()

	for _, cn := range toRemove {
		if err := e.Remove(ctx, instanceName, cn); err != nil {
			e.log.Warn("bandwidth remove", "instance", instanceName, "cn", cn, "err", err)
		}
	}

	if device == "" && len(desired) > 0 {
		e.log.Debug("bandwidth sync skipped: instance has no device name",
			"instance", instanceName, "clients", len(desired), "mode", e.mode)
		return nil
	}

	// Stable order for class ID assignment.
	cns := make([]string, 0, len(desired))
	for cn := range desired {
		cns = append(cns, cn)
	}
	sort.Strings(cns)

	for _, cn := range cns {
		cl := desired[cn]
		if err := e.Apply(ctx, instanceName, device, cl); err != nil {
			e.log.Warn("bandwidth apply", "instance", instanceName, "cn", cn, "err", err)
		}
	}
	return nil
}

// Apply installs shaping for one client (idempotent replace when limits change).
func (e *Enforcer) Apply(ctx context.Context, instanceName, device string, c ClientLimit) error {
	if e == nil || e.mode == ModeOff || e.mode == ModeShaper {
		return nil
	}
	if !NeedsShaping(c.StaticIP, c.RxBps, c.TxBps) {
		return e.Remove(ctx, instanceName, c.CommonName)
	}
	device = strings.TrimSpace(device)
	if device == "" {
		return fmt.Errorf("device required for bandwidth apply")
	}

	e.mu.Lock()
	if e.applied[instanceName] == nil {
		e.applied[instanceName] = make(map[string]appliedClient)
	}
	prev, had := e.applied[instanceName][c.CommonName]
	if had && prev.StaticIP == c.StaticIP && prev.RxBps == c.RxBps && prev.TxBps == c.TxBps && prev.Device == device {
		e.mu.Unlock()
		return nil // already applied
	}
	classID := uint32(0)
	if had {
		classID = prev.ClassID
	} else {
		classID = e.allocClassID(device)
	}
	// Drop previous rules if IP/device/class changed.
	oldRules := prev.Rules
	e.mu.Unlock()

	if had && len(oldRules) > 0 {
		_ = e.runRules(ctx, RemoveRules(oldRules), true)
	}

	rules := Plan(PlanInput{
		Device:   device,
		StaticIP: c.StaticIP,
		RxBps:    c.RxBps,
		TxBps:    c.TxBps,
		ClassID:  classID,
	})
	if len(rules) == 0 {
		return nil
	}

	// ModeTC: if tc is missing, soft no-op without recording so a later install can apply.
	if e.mode == ModeTC && !e.binAvailable("tc") {
		return nil
	}

	if err := e.runRules(ctx, ApplyRules(rules), false); err != nil {
		return err
	}

	e.mu.Lock()
	e.applied[instanceName][c.CommonName] = appliedClient{
		StaticIP: c.StaticIP,
		RxBps:    c.RxBps,
		TxBps:    c.TxBps,
		ClassID:  classID,
		Device:   device,
		Rules:    rules,
	}
	e.mu.Unlock()
	e.log.Info("bandwidth applied",
		"instance", instanceName, "cn", c.CommonName, "ip", c.StaticIP,
		"rx_bps", c.RxBps, "tx_bps", c.TxBps, "mode", e.mode)
	return nil
}

func (e *Enforcer) binAvailable(bin string) bool {
	e.mu.Lock()
	runner := e.runner
	e.mu.Unlock()
	if _, err := runner.LookPath(bin); err != nil {
		e.mu.Lock()
		if !e.missing[bin] {
			e.missing[bin] = true
			e.log.Warn("bandwidth binary missing; shaping no-op until available", "bin", bin)
		}
		e.mu.Unlock()
		return false
	}
	return true
}

// Remove clears shaping for one client.
func (e *Enforcer) Remove(ctx context.Context, instanceName, commonName string) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	instMap := e.applied[instanceName]
	prev, ok := instMap[commonName]
	if ok {
		delete(instMap, commonName)
	}
	e.mu.Unlock()
	if !ok {
		return nil
	}
	if err := e.runRules(ctx, RemoveRules(prev.Rules), true); err != nil {
		e.log.Warn("bandwidth remove rules", "instance", instanceName, "cn", commonName, "err", err)
		// still consider removed from bookkeeping
	}
	e.log.Info("bandwidth removed", "instance", instanceName, "cn", commonName, "ip", prev.StaticIP)
	return nil
}

// ClearInstance removes all shaping for an instance (orphan / disable).
func (e *Enforcer) ClearInstance(ctx context.Context, instanceName string) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	instMap := e.applied[instanceName]
	cns := make([]string, 0, len(instMap))
	for cn := range instMap {
		cns = append(cns, cn)
	}
	e.mu.Unlock()
	sort.Strings(cns)
	for _, cn := range cns {
		_ = e.Remove(ctx, instanceName, cn)
	}
	_ = e.ClearDevice(ctx, instanceName)
	e.mu.Lock()
	delete(e.applied, instanceName)
	e.mu.Unlock()
	return nil
}

// SyncDevice applies or clears whole-interface shaping for an instance.
// Used for client-role tunnels (primary) and optional server-role TUN ceiling.
// ModeShaper is confgen-only (no host commands).
func (e *Enforcer) SyncDevice(ctx context.Context, instanceName, device string, rxBps, txBps int64) error {
	if e == nil {
		return nil
	}
	switch e.mode {
	case ModeOff, ModeShaper:
		return nil
	case ModeTC, ModeLog:
	default:
		return nil
	}
	device = strings.TrimSpace(device)
	if !NeedsDeviceShaping(rxBps, txBps) || device == "" {
		return e.ClearDevice(ctx, instanceName)
	}

	e.mu.Lock()
	prev, had := e.devices[instanceName]
	if had && prev.Device == device && prev.RxBps == rxBps && prev.TxBps == txBps {
		e.mu.Unlock()
		return nil
	}
	oldRules := prev.Rules
	e.mu.Unlock()

	if had && len(oldRules) > 0 {
		_ = e.runRules(ctx, RemoveRules(oldRules), true)
	}

	rules := PlanDevice(DevicePlanInput{Device: device, RxBps: rxBps, TxBps: txBps})
	if len(rules) == 0 {
		return e.ClearDevice(ctx, instanceName)
	}
	if e.mode == ModeTC && !e.binAvailable("tc") {
		return nil
	}
	if err := e.runRules(ctx, ApplyRules(rules), false); err != nil {
		return err
	}
	e.mu.Lock()
	e.devices[instanceName] = appliedDevice{
		Device: device, RxBps: rxBps, TxBps: txBps, Rules: rules,
	}
	e.mu.Unlock()
	e.log.Info("bandwidth device applied",
		"instance", instanceName, "device", device,
		"rx_bps", rxBps, "tx_bps", txBps, "mode", e.mode)
	return nil
}

// ClearDevice removes whole-interface shaping for an instance.
func (e *Enforcer) ClearDevice(ctx context.Context, instanceName string) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	prev, ok := e.devices[instanceName]
	if ok {
		delete(e.devices, instanceName)
	}
	e.mu.Unlock()
	if !ok {
		return nil
	}
	if err := e.runRules(ctx, RemoveRules(prev.Rules), true); err != nil {
		e.log.Warn("bandwidth device remove", "instance", instanceName, "err", err)
	}
	e.log.Info("bandwidth device removed", "instance", instanceName, "device", prev.Device)
	return nil
}

// AppliedCount returns how many clients currently have rules for an instance (tests).
func (e *Enforcer) AppliedCount(instanceName string) int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.applied[instanceName])
}

func (e *Enforcer) allocClassID(device string) uint32 {
	id, ok := e.nextCls[device]
	if !ok || id < 10 {
		id = 10
	}
	e.nextCls[device] = id + 1
	// Avoid overflowing prio space we use for ingress (10000+id).
	if e.nextCls[device] > 9000 {
		e.nextCls[device] = 10
	}
	return id
}

func (e *Enforcer) runRules(ctx context.Context, rules []Rule, undo bool) error {
	for _, r := range rules {
		if r.Bin == "" {
			continue
		}
		switch e.mode {
		case ModeLog:
			e.log.Info("bandwidth plan", "undo", undo, "rule", r.String(), "desc", r.Desc)
			continue
		case ModeTC:
			// fall through
		default:
			continue
		}

		e.mu.Lock()
		runner := e.runner
		e.mu.Unlock()

		path, err := runner.LookPath(r.Bin)
		if err != nil {
			e.mu.Lock()
			if !e.missing[r.Bin] {
				e.missing[r.Bin] = true
				e.log.Warn("bandwidth binary missing; shaping no-op until available", "bin", r.Bin)
			}
			e.mu.Unlock()
			return nil // soft no-op
		}
		if err := runner.Run(ctx, path, r.Args...); err != nil {
			// On undo, missing filters/classes are common — log and continue.
			if undo {
				e.log.Debug("bandwidth undo rule failed", "rule", r.String(), "err", err)
				continue
			}
			return err
		}
	}
	return nil
}

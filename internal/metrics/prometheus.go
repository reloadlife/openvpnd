package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/reloadlife/openvpnd/internal/stats"
)

// Collector exports OpenVPN stats from the cache.
type Collector struct {
	cache      *stats.Cache
	up         prometheus.Gauge
	reconcile  *prometheus.HistogramVec
	reconcileE prometheus.Counter
}

// New registers process-level metrics and returns a collector that scrapes the cache.
func New(cache *stats.Cache, reg prometheus.Registerer) *Collector {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	c := &Collector{
		cache: cache,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "openvpnd_up",
			Help: "1 if openvpnd is up",
		}),
		reconcile: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "openvpnd_reconcile_duration_seconds",
			Help:    "Reconcile duration",
			Buckets: prometheus.DefBuckets,
		}, []string{}),
		reconcileE: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "openvpnd_reconcile_errors_total",
			Help: "Reconcile errors",
		}),
	}
	c.up.Set(1)
	_ = reg.Register(c.up)
	_ = reg.Register(c.reconcile)
	_ = reg.Register(c.reconcileE)
	_ = reg.Register(newCacheCollector(cache))
	return c
}

// ObserveReconcile records duration and error.
func (c *Collector) ObserveReconcile(d time.Duration, err error) {
	c.reconcile.WithLabelValues().Observe(d.Seconds())
	if err != nil {
		c.reconcileE.Inc()
	}
}

type cacheCollector struct {
	cache *stats.Cache

	instUp       *prometheus.Desc
	instRole     *prometheus.Desc
	instPort     *prometheus.Desc
	instClients  *prometheus.Desc
	instPID      *prometheus.Desc
	instRx       *prometheus.Desc
	instTx       *prometheus.Desc
	instRxBps    *prometheus.Desc
	instTxBps    *prometheus.Desc
	instInfo     *prometheus.Desc

	cliConn      *prometheus.Desc
	cliConnSince *prometheus.Desc
	cliSusp      *prometheus.Desc
	cliRx        *prometheus.Desc
	cliTx        *prometheus.Desc
	cliRxBps     *prometheus.Desc
	cliTxBps     *prometheus.Desc
	cliInfo      *prometheus.Desc
	cliReal      *prometheus.Desc
	cliVirt      *prometheus.Desc
}

func newCacheCollector(cache *stats.Cache) *cacheCollector {
	return &cacheCollector{
		cache: cache,
		instUp: prometheus.NewDesc("openvpn_instance_up", "Instance process up",
			[]string{"instance"}, nil),
		instRole: prometheus.NewDesc("openvpn_instance_info", "Instance metadata (always 1)",
			[]string{"instance", "role"}, nil),
		instPort: prometheus.NewDesc("openvpn_instance_listen_port", "Listen / configured port",
			[]string{"instance"}, nil),
		instClients: prometheus.NewDesc("openvpn_instance_connected_clients", "Connected client count (server)",
			[]string{"instance"}, nil),
		instPID: prometheus.NewDesc("openvpn_instance_pid", "Process ID",
			[]string{"instance"}, nil),
		instRx: prometheus.NewDesc("openvpn_instance_receive_bytes_total", "Instance RX bytes",
			[]string{"instance"}, nil),
		instTx: prometheus.NewDesc("openvpn_instance_transmit_bytes_total", "Instance TX bytes",
			[]string{"instance"}, nil),
		instRxBps: prometheus.NewDesc("openvpn_instance_receive_bytes_per_second", "Instance RX rate",
			[]string{"instance"}, nil),
		instTxBps: prometheus.NewDesc("openvpn_instance_transmit_bytes_per_second", "Instance TX rate",
			[]string{"instance"}, nil),
		instInfo: prometheus.NewDesc("openvpn_instance_last_error_info", "1 if last_error set (label carries message truncated)",
			[]string{"instance", "has_error"}, nil),

		cliConn: prometheus.NewDesc("openvpn_client_connected", "Client connected",
			[]string{"instance", "common_name"}, nil),
		cliConnSince: prometheus.NewDesc("openvpn_client_connected_since_seconds", "Connected since unix",
			[]string{"instance", "common_name"}, nil),
		cliSusp: prometheus.NewDesc("openvpn_client_suspended", "Client suspended",
			[]string{"instance", "common_name"}, nil),
		cliRx: prometheus.NewDesc("openvpn_client_receive_bytes_total", "Client RX bytes",
			[]string{"instance", "common_name"}, nil),
		cliTx: prometheus.NewDesc("openvpn_client_transmit_bytes_total", "Client TX bytes",
			[]string{"instance", "common_name"}, nil),
		cliRxBps: prometheus.NewDesc("openvpn_client_receive_bytes_per_second", "Client RX rate",
			[]string{"instance", "common_name"}, nil),
		cliTxBps: prometheus.NewDesc("openvpn_client_transmit_bytes_per_second", "Client TX rate",
			[]string{"instance", "common_name"}, nil),
		cliInfo: prometheus.NewDesc("openvpn_client_info", "Client info",
			[]string{"instance", "common_name", "name"}, nil),
		cliReal: prometheus.NewDesc("openvpn_client_real_address_info", "Real (public) address",
			[]string{"instance", "common_name", "real_address"}, nil),
		cliVirt: prometheus.NewDesc("openvpn_client_virtual_address_info", "Virtual (tunnel) address",
			[]string{"instance", "common_name", "virtual_address"}, nil),
	}
}

func (c *cacheCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{
		c.instUp, c.instRole, c.instPort, c.instClients, c.instPID,
		c.instRx, c.instTx, c.instRxBps, c.instTxBps, c.instInfo,
		c.cliConn, c.cliConnSince, c.cliSusp, c.cliRx, c.cliTx, c.cliRxBps, c.cliTxBps,
		c.cliInfo, c.cliReal, c.cliVirt,
	} {
		ch <- d
	}
}

func (c *cacheCollector) Collect(ch chan<- prometheus.Metric) {
	instances, clients := c.cache.SnapshotMaps()
	for name, inst := range instances {
		up := 0.0
		if inst.Up {
			up = 1
		}
		hasErr := "0"
		if inst.LastError != "" {
			hasErr = "1"
		}
		ch <- prometheus.MustNewConstMetric(c.instUp, prometheus.GaugeValue, up, name)
		ch <- prometheus.MustNewConstMetric(c.instRole, prometheus.GaugeValue, 1, name, inst.Role)
		ch <- prometheus.MustNewConstMetric(c.instPort, prometheus.GaugeValue, float64(inst.Port), name)
		ch <- prometheus.MustNewConstMetric(c.instClients, prometheus.GaugeValue, float64(inst.ConnectedClients), name)
		ch <- prometheus.MustNewConstMetric(c.instPID, prometheus.GaugeValue, float64(inst.PID), name)
		ch <- prometheus.MustNewConstMetric(c.instRx, prometheus.CounterValue, float64(inst.RxBytes), name)
		ch <- prometheus.MustNewConstMetric(c.instTx, prometheus.CounterValue, float64(inst.TxBytes), name)
		ch <- prometheus.MustNewConstMetric(c.instRxBps, prometheus.GaugeValue, inst.RxBps, name)
		ch <- prometheus.MustNewConstMetric(c.instTxBps, prometheus.GaugeValue, inst.TxBps, name)
		ch <- prometheus.MustNewConstMetric(c.instInfo, prometheus.GaugeValue, 1, name, hasErr)
	}
	for _, cl := range clients {
		labels := []string{cl.Instance, cl.CommonName}
		conn := 0.0
		if cl.Connected {
			conn = 1
		}
		susp := 0.0
		if cl.Suspended {
			susp = 1
		}
		connSince := 0.0
		if !cl.ConnectedSince.IsZero() {
			connSince = float64(cl.ConnectedSince.Unix())
		}
		ch <- prometheus.MustNewConstMetric(c.cliConn, prometheus.GaugeValue, conn, labels...)
		ch <- prometheus.MustNewConstMetric(c.cliConnSince, prometheus.GaugeValue, connSince, labels...)
		ch <- prometheus.MustNewConstMetric(c.cliSusp, prometheus.GaugeValue, susp, labels...)
		ch <- prometheus.MustNewConstMetric(c.cliRx, prometheus.CounterValue, float64(cl.RxBytes), labels...)
		ch <- prometheus.MustNewConstMetric(c.cliTx, prometheus.CounterValue, float64(cl.TxBytes), labels...)
		ch <- prometheus.MustNewConstMetric(c.cliRxBps, prometheus.GaugeValue, cl.RxBps, labels...)
		ch <- prometheus.MustNewConstMetric(c.cliTxBps, prometheus.GaugeValue, cl.TxBps, labels...)
		ch <- prometheus.MustNewConstMetric(c.cliInfo, prometheus.GaugeValue, 1, cl.Instance, cl.CommonName, cl.Name)
		if cl.RealAddress != "" {
			ch <- prometheus.MustNewConstMetric(c.cliReal, prometheus.GaugeValue, 1, cl.Instance, cl.CommonName, cl.RealAddress)
		}
		if cl.VirtualAddress != "" {
			ch <- prometheus.MustNewConstMetric(c.cliVirt, prometheus.GaugeValue, 1, cl.Instance, cl.CommonName, cl.VirtualAddress)
		}
	}
}

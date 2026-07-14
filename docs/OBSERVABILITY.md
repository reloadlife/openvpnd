# Observability (Prometheus + SNMP)

## Prometheus

Metrics are exposed at:

- `GET /metrics` on the main API listener (with other routes)
- Dedicated listener `listen.metrics` (default `127.0.0.1:9092`) — `/metrics` + `/healthz`

### Process metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `openvpnd_up` | gauge | — | Always 1 while process runs |
| `openvpnd_reconcile_duration_seconds` | histogram | — | Reconcile latency |
| `openvpnd_reconcile_errors_total` | counter | — | Failed reconcile cycles |

### Instance metrics

| Metric | Labels |
|--------|--------|
| `openvpn_instance_up` | `instance` |
| `openvpn_instance_info` | `instance`, `role` |
| `openvpn_instance_listen_port` | `instance` |
| `openvpn_instance_connected_clients` | `instance` |
| `openvpn_instance_pid` | `instance` |
| `openvpn_instance_receive_bytes_total` | `instance` |
| `openvpn_instance_transmit_bytes_total` | `instance` |
| `openvpn_instance_receive_bytes_per_second` | `instance` |
| `openvpn_instance_transmit_bytes_per_second` | `instance` |
| `openvpn_instance_last_error_info` | `instance`, `has_error` |

### Client metrics

| Metric | Labels |
|--------|--------|
| `openvpn_client_connected` | `instance`, `common_name` |
| `openvpn_client_connected_since_seconds` | `instance`, `common_name` |
| `openvpn_client_suspended` | `instance`, `common_name` |
| `openvpn_client_receive_bytes_total` | `instance`, `common_name` |
| `openvpn_client_transmit_bytes_total` | `instance`, `common_name` |
| `openvpn_client_receive_bytes_per_second` | `instance`, `common_name` |
| `openvpn_client_transmit_bytes_per_second` | `instance`, `common_name` |
| `openvpn_client_info` | `instance`, `common_name`, `name` |
| `openvpn_client_real_address_info` | `instance`, `common_name`, `real_address` |
| `openvpn_client_virtual_address_info` | `instance`, `common_name`, `virtual_address` |

### Scrape example

```yaml
# prometheus.yml
scrape_configs:
  - job_name: openvpnd
    static_configs:
      - targets: ["127.0.0.1:9092"]
```

```bash
curl -sS http://127.0.0.1:9092/metrics | grep openvpn
```

## SNMPv2c

Optional agent (disabled by default).

```yaml
snmp:
  enabled: true
  listen: "127.0.0.1:1162"
  community: "change-me-snmp"
  enterprise_oid: "1.3.6.1.4.1.66666.2"
```

MIB definition: [`deploy/mibs/OPENVPND-MIB.txt`](../deploy/mibs/OPENVPND-MIB.txt)

### OID map (base `1.3.6.1.4.1.66666.2`)

| OID | Meaning |
|-----|---------|
| `base.1.1.0` | Instance count |
| `base.1.2.0` | Client count |
| `base.1.3.0` | Agent uptime (TimeTicks) |
| `base.2.1.<col>.<row>` | Instance table |
| `base.3.1.<col>.<row>` | Client table |

Also exports SNMPv2-MIB `system` subset under `1.3.6.1.2.1.1.*`.

```bash
# with snmpwalk
snmpwalk -v2c -c change-me-snmp 127.0.0.1:1162 1.3.6.1.4.1.66666.2
```

**Security:** change the community; bind to localhost or firewall UDP; SET is rejected (`notWritable`).

## Port map vs wireguardd

| Service | wireguardd | openvpnd |
|---------|------------|----------|
| API | 51880 | 51980 |
| Prometheus | 9091 | 9092 |
| SNMP | 1161 | 1162 |
| Enterprise OID | `…66666.1` | `…66666.2` |

package ovpnbackend

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// mgmtConn is a unix management client.
type mgmtConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialMgmt(path string) (*mgmtConn, error) {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mgmt dial %s: %w", path, err)
	}
	m := &mgmtConn{conn: conn, r: bufio.NewReader(conn)}
	// Read greeting
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	for {
		line, err := m.r.ReadString('\n')
		if err != nil {
			break
		}
		if strings.Contains(line, "INFO:OpenVPN") || strings.HasPrefix(line, ">INFO:") {
			break
		}
		if len(line) == 0 {
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return m, nil
}

func (m *mgmtConn) Close() error {
	if m.conn != nil {
		return m.conn.Close()
	}
	return nil
}

func (m *mgmtConn) command(ctx context.Context, cmd string) ([]string, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = m.conn.SetDeadline(deadline)
	} else {
		_ = m.conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	defer func() { _ = m.conn.SetDeadline(time.Time{}) }()

	if _, err := fmt.Fprintf(m.conn, "%s\n", cmd); err != nil {
		return nil, err
	}
	var lines []string
	for {
		line, err := m.r.ReadString('\n')
		if err != nil {
			return lines, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "END" || strings.HasPrefix(line, "SUCCESS:") || strings.HasPrefix(line, "ERROR:") {
			if strings.HasPrefix(line, "ERROR:") {
				return lines, fmt.Errorf("mgmt: %s", line)
			}
			if line != "END" {
				lines = append(lines, line)
			}
			return lines, nil
		}
		lines = append(lines, line)
	}
}

func (m *mgmtConn) Status(ctx context.Context) (LiveInstance, error) {
	// status 2 is CSV-ish (server multi-client). Client mode often has no CLIENT_LIST;
	// fall back to plain "status" STATISTICS / byte counters.
	lines, err := m.command(ctx, "status 2")
	if err != nil && len(lines) == 0 {
		return LiveInstance{}, err
	}
	live := parseStatus2(lines)
	if live.RxBytes == 0 && live.TxBytes == 0 && len(live.Clients) == 0 {
		if lines2, err2 := m.command(ctx, "status"); err2 == nil || len(lines2) > 0 {
			if rx, tx, ok := parseClientStatistics(lines2); ok {
				live.RxBytes, live.TxBytes = rx, tx
			}
		}
	}
	// status 2 may also embed STATISTICS-style rows on some builds
	if live.RxBytes == 0 && live.TxBytes == 0 {
		if rx, tx, ok := parseClientStatistics(lines); ok {
			live.RxBytes, live.TxBytes = rx, tx
		}
	}
	live.Up = true
	live.UpdatedAt = time.Now().UTC()
	return live, nil
}

func parseStatus2(lines []string) LiveInstance {
	var live LiveInstance
	for _, line := range lines {
		if strings.HasPrefix(line, "CLIENT_LIST,") {
			// CLIENT_LIST,Common Name,Real Address,Virtual Address,Virtual IPv6 Address,Bytes Received,Bytes Sent,Connected Since,...
			parts := strings.Split(line, ",")
			if len(parts) < 8 {
				continue
			}
			rx, _ := strconv.ParseInt(parts[5], 10, 64)
			tx, _ := strconv.ParseInt(parts[6], 10, 64)
			since, _ := time.Parse(time.RFC3339, parts[7])
			if since.IsZero() {
				// older openvpn: "Mon Jan 2 15:04:05 2006"
				since, _ = time.Parse("Mon Jan 2 15:04:05 2006", parts[7])
			}
			c := LiveClient{
				CommonName:     parts[1],
				RealAddress:    parts[2],
				VirtualAddress: parts[3],
				ConnectedSince: since,
				RxBytes:        rx,
				TxBytes:        tx,
			}
			live.Clients = append(live.Clients, c)
			live.RxBytes += rx
			live.TxBytes += tx
		}
	}
	return live
}

// parseClientStatistics extracts tunnel byte counters from client-mode status output.
// Prefers TCP/UDP read/write bytes; falls back to TUN/TAP read/write bytes.
func parseClientStatistics(lines []string) (rx, tx int64, ok bool) {
	var tunR, tunW, udpR, udpW int64
	var haveTun, haveUDP bool
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// CSV: "TUN/TAP read bytes,123" or "TCP/UDP read bytes,456"
		key, val, found := strings.Cut(line, ",")
		if !found {
			// non-CSV: "TUN/TAP read bytes: 123"
			key, val, found = strings.Cut(line, ":")
			if !found {
				continue
			}
		}
		key = strings.TrimSpace(strings.ToLower(key))
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "tun/tap read bytes":
			tunR, haveTun = n, true
		case "tun/tap write bytes":
			tunW, haveTun = n, true
		case "tcp/udp read bytes":
			udpR, haveUDP = n, true
		case "tcp/udp write bytes":
			udpW, haveUDP = n, true
		}
	}
	if haveUDP {
		// OpenVPN: read = from peer (rx into us), write = to peer (tx out)
		return udpR, udpW, true
	}
	if haveTun {
		// TUN read = packets from kernel to tunnel (local→remote ≈ tx on wire inverse);
		// for display, map TUN read→tx (apps sending), TUN write→rx (apps receiving).
		return tunW, tunR, true
	}
	return 0, 0, false
}

func (m *mgmtConn) KillClient(ctx context.Context, cnOrAddr string) error {
	_, err := m.command(ctx, "kill "+cnOrAddr)
	return err
}

func (m *mgmtConn) Signal(ctx context.Context, sig string) error {
	_, err := m.command(ctx, "signal "+sig)
	return err
}

// Raw sends a management command and returns the response body as a single string.
// Lines are joined with "\n". SUCCESS lines are included; END is not.
func (m *mgmtConn) Raw(ctx context.Context, cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", fmt.Errorf("empty management command")
	}
	if strings.ContainsAny(cmd, "\r\n") {
		return "", fmt.Errorf("management command must be a single line")
	}
	lines, err := m.command(ctx, cmd)
	out := strings.Join(lines, "\n")
	return out, err
}

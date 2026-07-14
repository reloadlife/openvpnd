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
	// status 2 is CSV-ish
	lines, err := m.command(ctx, "status 2")
	if err != nil && len(lines) == 0 {
		return LiveInstance{}, err
	}
	var live LiveInstance
	var inClients bool
	for _, line := range lines {
		if strings.HasPrefix(line, "HEADER,CLIENT_LIST") {
			inClients = true
			continue
		}
		if strings.HasPrefix(line, "HEADER,") {
			inClients = false
			continue
		}
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
			_ = inClients
		}
	}
	live.Up = true
	live.UpdatedAt = time.Now().UTC()
	return live, nil
}

func (m *mgmtConn) KillClient(ctx context.Context, cnOrAddr string) error {
	_, err := m.command(ctx, "kill "+cnOrAddr)
	return err
}

func (m *mgmtConn) Signal(ctx context.Context, sig string) error {
	_, err := m.command(ctx, "signal "+sig)
	return err
}

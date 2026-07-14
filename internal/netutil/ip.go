package netutil

import (
	"fmt"
	"net"
	"strings"
)

// ValidateCIDR checks a single CIDR.
func ValidateCIDR(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("empty CIDR")
	}
	ip, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return fmt.Errorf("invalid CIDR %q: %w", s, err)
	}
	if ip == nil || ipnet == nil {
		return fmt.Errorf("invalid CIDR %q", s)
	}
	return nil
}

// ValidateIP accepts a bare IP.
func ValidateIP(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("empty IP")
	}
	if net.ParseIP(s) == nil {
		return fmt.Errorf("invalid IP %q", s)
	}
	return nil
}

// NormalizeHostIP turns bare IP or host CIDR into a host address string.
func NormalizeHostIP(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty")
	}
	if strings.Contains(s, "/") {
		ip, _, err := net.ParseCIDR(s)
		if err != nil {
			return "", err
		}
		return ip.String(), nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return "", fmt.Errorf("invalid IP %q", s)
	}
	return ip.String(), nil
}

// ServerNetworkToOpenVPN converts "10.8.0.0/24" → network + netmask for --server.
func ServerNetworkToOpenVPN(cidr string) (network, netmask string, err error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return "", "", fmt.Errorf("empty server network")
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", fmt.Errorf("invalid server network %q: %w", cidr, err)
	}
	if ipnet.IP.To4() == nil {
		return "", "", fmt.Errorf("only IPv4 server networks supported currently")
	}
	return ipnet.IP.To4().String(), net.IP(ipnet.Mask).String(), nil
}

// AllocateNextHost picks the next free host in serverNetwork, skipping used and .1 (server).
func AllocateNextHost(serverNetwork string, used []string) (string, error) {
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(serverNetwork))
	if err != nil {
		return "", fmt.Errorf("invalid server network: %w", err)
	}
	if ipnet.IP.To4() == nil {
		return "", fmt.Errorf("only IPv4 pools supported currently")
	}
	usedSet := map[string]struct{}{}
	for _, u := range used {
		if h, err := NormalizeHostIP(u); err == nil {
			usedSet[h] = struct{}{}
		}
	}
	// Mark network, broadcast, and typical server .1 as used.
	netIP := ipnet.IP.To4()
	usedSet[netIP.String()] = struct{}{}
	bcast := lastIPv4(ipnet)
	usedSet[bcast.String()] = struct{}{}
	serverIP := make(net.IP, 4)
	copy(serverIP, netIP)
	incIP(serverIP) // .1
	usedSet[serverIP.String()] = struct{}{}

	ip := make(net.IP, 4)
	copy(ip, netIP)
	incIP(ip) // start at .1
	incIP(ip) // prefer .2 first
	for !ip.Equal(bcast) {
		s := ip.String()
		if _, ok := usedSet[s]; !ok {
			return s, nil
		}
		incIP(ip)
	}
	return "", fmt.Errorf("no free host IP left in %s", serverNetwork)
}

func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

func lastIPv4(n *net.IPNet) net.IP {
	ip := make(net.IP, 4)
	copy(ip, n.IP.To4())
	for i := 0; i < 4; i++ {
		ip[i] |= ^n.Mask[i]
	}
	return ip
}

// IsAutoToken reports whether the user asked for auto-generation.
func IsAutoToken(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "" || s == "auto" || s == "next" || s == "*"
}

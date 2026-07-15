package confimport

import (
	"fmt"
	"strings"

	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

// ToCreateRequest maps a parse Result into an InstanceCreateRequest.
// Typed fields that are not yet first-class on the create API are folded
// into ExtraDirectives (max-clients, tls-version-min, tun-mtu, buffers, server-ipv6).
func (r Result) ToCreateRequest() pkgapi.InstanceCreateRequest {
	req := pkgapi.InstanceCreateRequest{
		Role:            r.Role,
		DevType:         r.DevType,
		Device:          r.Device,
		Proto:           r.Proto,
		LocalBind:       r.LocalBind,
		Port:            r.Port,
		ServerNetwork:   r.ServerNetwork,
		Topology:        r.Topology,
		AuthMode:        r.AuthMode,
		Cipher:          r.Cipher,
		DataCiphers:     r.DataCiphers,
		AuthDigest:      r.AuthDigest,
		PushRoutes:      append([]string(nil), r.PushRoutes...),
		PushDNS:         append([]string(nil), r.PushDNS...),
		PushDomain:      r.PushDomain,
		RedirectGateway: r.RedirectGateway,
		PKICaPath:       r.PKICaPath,
		PKICertPath:     r.PKICertPath,
		PKIKeyPath:      r.PKIKeyPath,
		PKITLSCryptPath: r.TLSCryptPath,
		PKIDHPath:       r.PKIDHPath,
		StaticKeyPath:   r.StaticKeyPath,
	}

	if len(r.Remotes) > 0 {
		req.Remotes = make([]pkgapi.Remote, 0, len(r.Remotes))
		for _, rem := range r.Remotes {
			req.Remotes = append(req.Remotes, pkgapi.Remote{
				Host: rem.Host, Port: rem.Port, Proto: rem.Proto,
			})
		}
	}
	if len(r.Plugins) > 0 {
		req.Plugins = make([]pkgapi.Plugin, 0, len(r.Plugins))
		for _, pl := range r.Plugins {
			req.Plugins = append(req.Plugins, pkgapi.Plugin{Path: pl.Path, Args: append([]string(nil), pl.Args...)})
		}
	}

	// Fold non-create-request fields into extra_directives.
	var extra strings.Builder
	if strings.TrimSpace(r.ExtraDirectives) != "" {
		extra.WriteString(strings.TrimRight(r.ExtraDirectives, "\n"))
		extra.WriteByte('\n')
	}
	if r.MaxClients > 0 {
		fmt.Fprintf(&extra, "max-clients %d\n", r.MaxClients)
	}
	if r.TLSVersionMin != "" {
		fmt.Fprintf(&extra, "tls-version-min %s\n", r.TLSVersionMin)
	}
	if r.TunMTU > 0 {
		fmt.Fprintf(&extra, "tun-mtu %d\n", r.TunMTU)
	}
	// Allow 0 for sndbuf/rcvbuf (OpenVPN uses 0 = OS default) only if explicitly set.
	// We use presence via >0 or tracking — Result uses 0 as unset; OpenVPN 0 is rare.
	// Prefer preserving only positive values and explicit 0 when set is hard without pointer.
	// Task lists Sndbuf/Rcvbuf on Result; fold non-zero, and also 0 is meaningful —
	// keep both when non-zero for simplicity (0 means unset in Result).
	if r.Sndbuf != 0 {
		fmt.Fprintf(&extra, "sndbuf %d\n", r.Sndbuf)
	}
	if r.Rcvbuf != 0 {
		fmt.Fprintf(&extra, "rcvbuf %d\n", r.Rcvbuf)
	}
	if r.ServerIPv6 != "" {
		fmt.Fprintf(&extra, "server-ipv6 %s\n", r.ServerIPv6)
	}
	req.ExtraDirectives = extra.String()

	// If conf already supplies leaf certs, do not auto-issue on create.
	if r.PKICertPath != "" && r.PKIKeyPath != "" {
		f := false
		req.IssueServerCert = &f
		req.GenerateTLSCrypt = &f
	}

	return req
}

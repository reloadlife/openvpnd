package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/netutil"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) handleListClients(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	list, err := s.store.ListClientsByInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.ServerClient, 0, len(list))
	for _, c := range list {
		out = append(out, s.toAPIClient(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	inst, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if inst.Role != "server" {
		writeError(w, http.StatusBadRequest, "bad_request", "clients only apply to server instances")
		return
	}
	var req pkgapi.ClientCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	cn := strings.TrimSpace(req.CommonName)
	if cn == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "common_name required")
		return
	}
	if err := validateClientCN(cn); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	var auto []string
	var warnings []string

	display := strings.TrimSpace(req.Name)
	if display == "" {
		display = cn
		auto = append(auto, "name="+display)
	}

	// Auto static IP from pool when empty/auto
	staticIP := strings.TrimSpace(req.StaticIP)
	if netutil.IsAutoToken(staticIP) {
		if inst.ServerNetwork == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "server has no server_network; cannot auto-allocate static_ip")
			return
		}
		used, err := s.store.ListUsedStaticIPs(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		ip, err := netutil.AllocateNextHost(inst.ServerNetwork, used)
		if err != nil {
			writeError(w, http.StatusConflict, "pool_exhausted", err.Error())
			return
		}
		staticIP = ip
		auto = append(auto, "static_ip="+staticIP)
	} else if staticIP != "" {
		if err := netutil.ValidateIP(staticIP); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}

	// Decide cert issuance: default ON when no manual paths and a CA is available.
	noManualCert := strings.TrimSpace(req.ClientCertPath) == "" && strings.TrimSpace(req.ClientKeyPath) == ""
	caName := strings.TrimSpace(req.CAName)
	if caName == "" {
		caName = s.preferCAForInstance(r, inst)
	}
	hasCA := caName != ""
	issue := false
	if req.IssueCert != nil {
		issue = *req.IssueCert
	} else if noManualCert && hasCA {
		issue = true
		auto = append(auto, "issue_cert=true")
	}
	if issue && !hasCA {
		writeError(w, http.StatusBadRequest, "no_ca",
			"issue_cert requires a CA — create one (POST /v1/pki/cas) or create the server with create_ca_if_empty")
		return
	}
	if issue && !noManualCert {
		writeError(w, http.StatusBadRequest, "bad_request", "issue_cert cannot be combined with client_cert_path/client_key_path")
		return
	}

	c, err := s.store.CreateClient(r.Context(), name, db.Client{
		CommonName: cn, Name: display, Notes: req.Notes,
		StaticIP: staticIP, PushRoutes: req.PushRoutes, IRoutes: req.IRoutes,
		PushDNS: req.PushDNS, PushDomain: req.PushDomain, RedirectGateway: req.RedirectGateway,
		DisablePush: req.DisablePush, Suspended: req.Suspended,
		TrafficLimitBytes: req.TrafficLimitBytes, BandwidthRxBps: req.BandwidthRxBps, BandwidthTxBps: req.BandwidthTxBps,
		CertRef: req.CertRef, ClientCertPath: req.ClientCertPath, ClientKeyPath: req.ClientKeyPath, Tags: req.Tags,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "create_failed", err.Error())
		return
	}

	if issue {
		issued, rec, err := s.issueAndStore(r, caName, "client", c.CommonName, 0, nil, nil, "", name)
		if err != nil {
			// Roll back client so we do not leave half-provisioned users.
			_ = s.store.DeleteClient(r.Context(), name, c.CommonName)
			writeError(w, http.StatusBadRequest, "issue_failed", err.Error())
			return
		}
		c.ClientCertPath = issued.CertPath
		c.ClientKeyPath = issued.KeyPath
		c.CertRef = fmt.Sprintf("%s/%d", rec.CAName, rec.ID)
		if updated, err := s.store.UpdateClient(r.Context(), name, c.CommonName, c); err == nil {
			c = updated
		}
		auto = append(auto, "cert="+c.CertRef)
	}

	_ = s.store.AddEvent(r.Context(), "info", "create", name, c.CommonName, "client created", "{}")
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetClient(r.Context(), name, c.CommonName)
	if fresh != nil {
		c = *fresh
	}

	resp := pkgapi.ClientCreateResponse{
		ServerClient: s.toAPIClient(c),
		AutoFilled:   auto,
	}

	// One-click profile link (optional)
	if req.MintProfileLink {
		link, warn, err := s.mintProfileLinkForClient(r, name, c.CommonName, req)
		if err != nil {
			warnings = append(warnings, "profile_link: "+err.Error())
		} else {
			resp.ProfileLink = link
			auto = append(auto, "profile_link")
			resp.AutoFilled = auto
		}
		if warn != "" {
			warnings = append(warnings, warn)
		}
	} else if c.ClientCertPath == "" || c.ClientKeyPath == "" {
		warnings = append(warnings, "no client cert yet — issue_cert or set paths before exporting .ovpn")
	} else if strings.TrimSpace(inst.PublicEndpoint) == "" {
		warnings = append(warnings, "set instance public_endpoint to generate installable profiles")
	}

	resp.Warnings = warnings
	writeJSON(w, http.StatusCreated, resp)
}

func validateClientCN(cn string) error {
	if len(cn) > 64 {
		return fmt.Errorf("common_name too long (max 64)")
	}
	for _, r := range cn {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '@' {
			continue
		}
		return fmt.Errorf("common_name has invalid character %q (use letters, digits, . _ - @)", r)
	}
	return nil
}

func (s *Server) preferCAForInstance(r *http.Request, inst *db.Instance) string {
	cas, err := s.store.ListCAs(r.Context())
	if err != nil || len(cas) == 0 {
		return ""
	}
	if inst != nil && inst.PKICaPath != "" {
		for _, c := range cas {
			if c.CertPath == inst.PKICaPath {
				return c.Name
			}
		}
	}
	return cas[0].Name
}

func (s *Server) mintProfileLinkForClient(r *http.Request, instance, cn string, req pkgapi.ClientCreateRequest) (*pkgapi.ProfileLink, string, error) {
	if _, _, err := s.renderClientOVPN(r.Context(), instance, cn); err != nil {
		return nil, "", err
	}
	ttl := s.cfg.ProfileLinkTTL()
	if req.ProfileLinkTTL != "" {
		d, err := time.ParseDuration(req.ProfileLinkTTL)
		if err != nil || d <= 0 {
			return nil, "", fmt.Errorf("invalid profile_link_ttl")
		}
		ttl = d
	}
	maxUses := s.cfg.ProfileLinks.DefaultMaxUses
	if req.ProfileLinkMaxUses != nil {
		maxUses = *req.ProfileLinkMaxUses
	}
	cli, err := s.store.GetClient(r.Context(), instance, cn)
	if err != nil {
		return nil, "", err
	}
	pt, err := s.store.CreateProfileToken(r.Context(), cli.ID, cli.InstanceID, ttl, maxUses, req.ProfileLinkNote)
	if err != nil {
		return nil, "", err
	}
	link := s.toProfileLink(pt)
	_ = s.store.AddEvent(r.Context(), "info", "profile_link", instance, cn,
		"profile link created on client create", fmt.Sprintf(`{"expires_at":%q,"max_uses":%d}`, pt.ExpiresAt.Format(time.RFC3339), maxUses))
	return &link, "", nil
}

func (s *Server) handleGetClient(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	c, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIClient(*c))
}

func (s *Server) handleUpdateClient(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	existing, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var req pkgapi.ClientUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	c := *existing
	if req.Name != nil {
		c.Name = *req.Name
	}
	if req.Notes != nil {
		c.Notes = *req.Notes
	}
	if req.StaticIP != nil {
		c.StaticIP = *req.StaticIP
	}
	if req.PushRoutes != nil {
		c.PushRoutes = req.PushRoutes
	}
	if req.IRoutes != nil {
		c.IRoutes = req.IRoutes
	}
	if req.PushDNS != nil {
		c.PushDNS = req.PushDNS
	}
	if req.PushDomain != nil {
		c.PushDomain = *req.PushDomain
	}
	if req.RedirectGateway != nil {
		c.RedirectGateway = *req.RedirectGateway
	}
	if req.DisablePush != nil {
		c.DisablePush = req.DisablePush
	}
	if req.Suspended != nil {
		c.Suspended = *req.Suspended
	}
	if req.TrafficLimitBytes != nil {
		c.TrafficLimitBytes = *req.TrafficLimitBytes
	}
	if req.BandwidthRxBps != nil {
		c.BandwidthRxBps = *req.BandwidthRxBps
	}
	if req.BandwidthTxBps != nil {
		c.BandwidthTxBps = *req.BandwidthTxBps
	}
	if req.CertRef != nil {
		c.CertRef = *req.CertRef
	}
	if req.ClientCertPath != nil {
		c.ClientCertPath = *req.ClientCertPath
	}
	if req.ClientKeyPath != nil {
		c.ClientKeyPath = *req.ClientKeyPath
	}
	if req.Tags != nil {
		c.Tags = req.Tags
	}
	out, err := s.store.UpdateClient(r.Context(), name, cn, c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	writeJSON(w, http.StatusOK, s.toAPIClient(out))
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	if err := s.store.DeleteClient(r.Context(), name, cn); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	_ = s.store.AddEvent(r.Context(), "info", "delete", name, cn, "client deleted", "{}")
	_ = s.ForceReconcile(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClientSuspend(w http.ResponseWriter, r *http.Request) {
	s.setClientSuspended(w, r, true)
}

func (s *Server) handleClientResume(w http.ResponseWriter, r *http.Request) {
	s.setClientSuspended(w, r, false)
}

func (s *Server) setClientSuspended(w http.ResponseWriter, r *http.Request, suspended bool) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	c, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err := s.store.SetClientSuspended(r.Context(), c.ID, suspended); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if suspended {
		if mgmt, err := s.backend.Management(r.Context(), name); err == nil {
			_ = mgmt.KillClient(r.Context(), cn)
			_ = mgmt.Close()
		}
	}
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetClient(r.Context(), name, cn)
	if fresh == nil {
		writeError(w, http.StatusNotFound, "not_found", "client gone")
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIClient(*fresh))
}

func (s *Server) handleClientResetTraffic(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	c, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if err := s.store.ResetClientTraffic(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	fresh, _ := s.store.GetClient(r.Context(), name, cn)
	if fresh == nil {
		writeError(w, http.StatusNotFound, "not_found", "client gone")
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIClient(*fresh))
}

func (s *Server) toAPIClient(c db.Client) pkgapi.ServerClient {
	connected := c.RealAddress != "" || c.VirtualAddress != ""
	if s.cache != nil {
		for _, st := range s.cache.ListClients() {
			if st.Instance == c.InstanceName && st.CommonName == c.CommonName {
				connected = st.Connected
				break
			}
		}
	}
	return pkgapi.ServerClient{
		ID: c.ID, InstanceID: c.InstanceID, InstanceName: c.InstanceName,
		CommonName: c.CommonName, Name: c.Name, Notes: c.Notes,
		StaticIP: c.StaticIP, PushRoutes: c.PushRoutes, IRoutes: c.IRoutes,
		PushDNS: c.PushDNS, PushDomain: c.PushDomain, RedirectGateway: c.RedirectGateway,
		DisablePush: c.DisablePush,
		Suspended: c.Suspended, Connected: connected,
		TrafficLimitBytes: c.TrafficLimitBytes, BandwidthRxBps: c.BandwidthRxBps, BandwidthTxBps: c.BandwidthTxBps,
		CertRef: c.CertRef, ClientCertPath: c.ClientCertPath, ClientKeyPath: c.ClientKeyPath,
		RealAddress: c.RealAddress, VirtualAddress: c.VirtualAddress,
		ConnectedSince: c.ConnectedSince,
		RxBytes:        c.EffectiveRx(), TxBytes: c.EffectiveTx(),
		RxBps: c.LastRxBps, TxBps: c.LastTxBps, Tags: c.Tags,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

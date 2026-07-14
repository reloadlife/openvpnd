package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/netutil"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListInstances(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.Instance, 0, len(list))
	for _, i := range list {
		out = append(out, s.toAPIInstance(i))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.InstanceCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role != "server" && role != "client" {
		writeError(w, http.StatusBadRequest, "bad_request", "role must be server or client")
		return
	}
	if role == "server" && req.ServerNetwork != "" {
		if err := netutil.ValidateCIDR(req.ServerNetwork); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	inst := db.Instance{
		Name: req.Name, Role: role, Enabled: enabled,
		BinaryName: req.BinaryName, BinaryPath: req.BinaryPath,
		DevType: req.DevType, Device: req.Device, Proto: req.Proto,
		LocalBind: req.LocalBind, Port: req.Port,
		Remotes: toDBRemotes(req.Remotes),
		ServerNetwork: req.ServerNetwork, Topology: req.Topology,
		PoolStart: req.PoolStart, PoolEnd: req.PoolEnd,
		AuthMode: req.AuthMode, Cipher: req.Cipher, DataCiphers: req.DataCiphers, AuthDigest: req.AuthDigest,
		PushRoutes: req.PushRoutes, PushDNS: req.PushDNS, PushDomain: req.PushDomain,
		RedirectGateway: req.RedirectGateway,
		PKICaPath: req.PKICaPath, PKICertPath: req.PKICertPath, PKIKeyPath: req.PKIKeyPath,
		PKITLSCryptPath: req.PKITLSCryptPath, PKIDHPath: req.PKIDHPath, StaticKeyPath: req.StaticKeyPath,
		ExtraDirectives: req.ExtraDirectives,
		PreUp: req.PreUp, PostUp: req.PostUp, PreDown: req.PreDown, PostDown: req.PostDown,
		PublicEndpoint: req.PublicEndpoint,
	}
	if inst.BinaryName == "" {
		inst.BinaryName = s.cfg.OpenVPN.DefaultBinary
	}
	out, err := s.store.CreateInstance(r.Context(), inst)
	if err != nil {
		writeError(w, http.StatusConflict, "create_failed", err.Error())
		return
	}
	_ = s.store.AddEvent(r.Context(), "info", "create", out.Name, "", "instance created", "{}")
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetInstance(r.Context(), out.Name)
	if fresh != nil {
		out = *fresh
	}
	writeJSON(w, http.StatusCreated, s.toAPIInstance(out))
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	inst, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIInstance(*inst))
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	existing, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var req pkgapi.InstanceUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	inst := *existing
	if req.Enabled != nil {
		inst.Enabled = *req.Enabled
	}
	if req.BinaryName != nil {
		inst.BinaryName = *req.BinaryName
	}
	if req.BinaryPath != nil {
		inst.BinaryPath = *req.BinaryPath
	}
	if req.DevType != nil {
		inst.DevType = *req.DevType
	}
	if req.Device != nil {
		inst.Device = *req.Device
	}
	if req.Proto != nil {
		inst.Proto = *req.Proto
	}
	if req.LocalBind != nil {
		inst.LocalBind = *req.LocalBind
	}
	if req.Port != nil {
		inst.Port = *req.Port
	}
	if req.Remotes != nil {
		inst.Remotes = toDBRemotes(req.Remotes)
	}
	if req.ServerNetwork != nil {
		inst.ServerNetwork = *req.ServerNetwork
	}
	if req.Topology != nil {
		inst.Topology = *req.Topology
	}
	if req.PoolStart != nil {
		inst.PoolStart = *req.PoolStart
	}
	if req.PoolEnd != nil {
		inst.PoolEnd = *req.PoolEnd
	}
	if req.AuthMode != nil {
		inst.AuthMode = *req.AuthMode
	}
	if req.Cipher != nil {
		inst.Cipher = *req.Cipher
	}
	if req.DataCiphers != nil {
		inst.DataCiphers = *req.DataCiphers
	}
	if req.AuthDigest != nil {
		inst.AuthDigest = *req.AuthDigest
	}
	if req.PushRoutes != nil {
		inst.PushRoutes = req.PushRoutes
	}
	if req.PushDNS != nil {
		inst.PushDNS = req.PushDNS
	}
	if req.PushDomain != nil {
		inst.PushDomain = *req.PushDomain
	}
	if req.RedirectGateway != nil {
		inst.RedirectGateway = *req.RedirectGateway
	}
	if req.PKICaPath != nil {
		inst.PKICaPath = *req.PKICaPath
	}
	if req.PKICertPath != nil {
		inst.PKICertPath = *req.PKICertPath
	}
	if req.PKIKeyPath != nil {
		inst.PKIKeyPath = *req.PKIKeyPath
	}
	if req.PKITLSCryptPath != nil {
		inst.PKITLSCryptPath = *req.PKITLSCryptPath
	}
	if req.PKIDHPath != nil {
		inst.PKIDHPath = *req.PKIDHPath
	}
	if req.StaticKeyPath != nil {
		inst.StaticKeyPath = *req.StaticKeyPath
	}
	if req.ExtraDirectives != nil {
		inst.ExtraDirectives = *req.ExtraDirectives
	}
	if req.PreUp != nil {
		inst.PreUp = *req.PreUp
	}
	if req.PostUp != nil {
		inst.PostUp = *req.PostUp
	}
	if req.PreDown != nil {
		inst.PreDown = *req.PreDown
	}
	if req.PostDown != nil {
		inst.PostDown = *req.PostDown
	}
	if req.PublicEndpoint != nil {
		inst.PublicEndpoint = *req.PublicEndpoint
	}
	out, err := s.store.UpdateInstance(r.Context(), inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetInstance(r.Context(), out.Name)
	if fresh != nil {
		out = *fresh
	}
	writeJSON(w, http.StatusOK, s.toAPIInstance(out))
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.backend.RemoveInstance(r.Context(), name); err != nil {
		s.log.Warn("remove instance backend", "name", name, "err", err)
	}
	if err := s.store.DeleteInstance(r.Context(), name); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	_ = s.store.AddEvent(r.Context(), "info", "delete", name, "", "instance deleted", "{}")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInstanceUp(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.store.SetInstanceEnabled(r.Context(), name, true); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	inst, _ := s.store.GetInstance(r.Context(), name)
	if inst == nil {
		writeError(w, http.StatusNotFound, "not_found", "instance gone")
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIInstance(*inst))
}

func (s *Server) handleInstanceDown(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.store.SetInstanceEnabled(r.Context(), name, false); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	inst, _ := s.store.GetInstance(r.Context(), name)
	if inst == nil {
		writeError(w, http.StatusNotFound, "not_found", "instance gone")
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIInstance(*inst))
}

func (s *Server) handleInstanceRestart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	inst, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	// force conf hash change via temporary extra space strip is fragile; stop then start
	_ = s.backend.StopInstance(r.Context(), name)
	// clear conf hash so Ensure restarts
	inst.ConfHash = ""
	_, _ = s.store.UpdateInstance(r.Context(), *inst)
	_ = s.store.SetInstanceEnabled(r.Context(), name, true)
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetInstance(r.Context(), name)
	if fresh == nil {
		writeError(w, http.StatusNotFound, "not_found", "instance gone")
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIInstance(*fresh))
}

func (s *Server) handleInstanceExport(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	inst, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var clients []db.Client
	if inst.Role == "server" {
		clients, _ = s.store.ListClientsByInstance(r.Context(), name)
	}
	paths := confgen.Paths{
		ConfDir:    s.cfg.OpenVPN.ConfDir,
		RuntimeDir: s.cfg.OpenVPN.RuntimeDir,
		Name:       name,
	}
	res, err := confgen.RenderInstance(*inst, paths, clients)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(res.Content))
}

func (s *Server) toAPIInstance(i db.Instance) pkgapi.Instance {
	up := false
	if s.cache != nil {
		if st, ok := s.cache.GetInstance(i.Name); ok {
			up = st.Up
		}
	}
	return pkgapi.Instance{
		ID: i.ID, Name: i.Name, Role: i.Role, Enabled: i.Enabled, Up: up,
		BinaryName: i.BinaryName, BinaryPath: i.BinaryPath,
		DevType: i.DevType, Device: i.Device, Proto: i.Proto,
		LocalBind: i.LocalBind, Port: i.Port,
		Remotes: toAPIRemotes(i.Remotes),
		ServerNetwork: i.ServerNetwork, Topology: i.Topology,
		PoolStart: i.PoolStart, PoolEnd: i.PoolEnd,
		AuthMode: i.AuthMode, Cipher: i.Cipher, DataCiphers: i.DataCiphers, AuthDigest: i.AuthDigest,
		PushRoutes: i.PushRoutes, PushDNS: i.PushDNS, PushDomain: i.PushDomain,
		RedirectGateway: i.RedirectGateway,
		PKICaPath: i.PKICaPath, PKICertPath: i.PKICertPath, PKIKeyPath: i.PKIKeyPath,
		PKITLSCryptPath: i.PKITLSCryptPath, PKIDHPath: i.PKIDHPath, StaticKeyPath: i.StaticKeyPath,
		ExtraDirectives: i.ExtraDirectives,
		PreUp: i.PreUp, PostUp: i.PostUp, PreDown: i.PreDown, PostDown: i.PostDown,
		PublicEndpoint: i.PublicEndpoint,
		PID: i.PID, LastError: i.LastError, ConnectedClients: i.ConnectedClients,
		RxBytes: i.LastRxBytes, TxBytes: i.LastTxBytes, RxBps: i.LastRxBps, TxBps: i.LastTxBps,
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
	}
}

func toDBRemotes(in []pkgapi.Remote) []db.Remote {
	if in == nil {
		return nil
	}
	out := make([]db.Remote, 0, len(in))
	for _, r := range in {
		out = append(out, db.Remote{Host: r.Host, Port: r.Port, Proto: r.Proto})
	}
	return out
}

func toAPIRemotes(in []db.Remote) []pkgapi.Remote {
	if in == nil {
		return nil
	}
	out := make([]pkgapi.Remote, 0, len(in))
	for _, r := range in {
		out = append(out, pkgapi.Remote{Host: r.Host, Port: r.Port, Proto: r.Proto})
	}
	return out
}

package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/instance"
	"github.com/reloadlife/openvpnd/internal/pki"
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

	ctxBuild, err := s.buildInstanceContext(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	in, err := createInputFromAPI(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// Default automation flags for servers when not specified
	if in.Role == "server" || in.Role == "" {
		if req.IssueServerCert != nil {
			in.IssueServerCert = *req.IssueServerCert
		}
		if req.GenerateTLSCrypt != nil {
			in.GenerateTLSCrypt = *req.GenerateTLSCrypt
		}
		in.CreateCAIfEmpty = req.CreateCAIfEmpty
		// If paths empty and flags nil, Prepare will auto-issue when CA exists
	}

	prepared, err := instance.Prepare(in, ctxBuild)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	out, err := s.store.CreateInstance(r.Context(), prepared.Instance)
	if err != nil {
		writeError(w, http.StatusConflict, "create_failed", err.Error())
		return
	}

	// Post-create: CA + server cert + tls-crypt
	if prepared.IssueServerCert {
		caName := prepared.CAName
		if prepared.CreateCAIfEmpty || caName == "" {
			if _, err := s.store.GetCA(r.Context(), firstNonEmpty(caName, "default")); err != nil {
				// create CA
				name := firstNonEmpty(caName, "default")
				mgr, err := pki.NewManager(s.cfg.OpenVPN.PKIDir)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "pki_error", err.Error())
					return
				}
				mat, err := mgr.CreateCA(pki.CreateCAOptions{
					Name: name, CommonName: "OpenVPNd CA " + name, ValidDays: 3650,
				})
				if err != nil {
					// may already exist on disk
					if cp, kp, e2 := mgr.CAPaths(name); e2 == nil {
						_, _ = s.store.UpsertCA(r.Context(), db.CA{
							Name: name, CommonName: name, CertPath: cp, KeyPath: kp, SerialNext: 2,
						})
					} else {
						writeError(w, http.StatusBadRequest, "pki_error", "create CA: "+err.Error())
						return
					}
				} else {
					_, _ = s.store.UpsertCA(r.Context(), db.CA{
						Name: mat.Name, CommonName: mat.CN, CertPath: mat.CertPath, KeyPath: mat.KeyPath,
						NotAfter: mat.NotAfter.UTC().Format(time.RFC3339), SerialNext: mat.Serial,
					})
					prepared.Auto = append(prepared.Auto, "created_ca="+mat.Name)
				}
				caName = name
			} else {
				caName = firstNonEmpty(caName, "default")
			}
		}
		dns := []string{prepared.ServerCN}
		issued, _, err := s.issueAndStore(r, caName, "server", prepared.ServerCN, 825, dns, nil, "", out.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "pki_error", "instance created but cert issue failed: "+err.Error())
			return
		}
		ca, _ := s.store.GetCA(r.Context(), caName)
		fresh, _ := s.store.GetInstance(r.Context(), out.Name)
		if fresh != nil && ca != nil {
			fresh.PKICaPath = ca.CertPath
			fresh.PKICertPath = issued.CertPath
			fresh.PKIKeyPath = issued.KeyPath
			if prepared.GenerateTLSCrypt {
				mgr, _ := pki.NewManager(s.cfg.OpenVPN.PKIDir)
				if mgr != nil {
					if path, err := mgr.GenerateTLSCrypt(out.Name); err == nil {
						_, _ = s.store.UpsertTLSCrypt(r.Context(), out.Name, path)
						fresh.PKITLSCryptPath = path
						prepared.Auto = append(prepared.Auto, "tls_crypt="+path)
					}
				}
			}
			out, _ = s.store.UpdateInstance(r.Context(), *fresh)
		}
	}

	_ = s.store.AddEvent(r.Context(), "info", "create", out.Name, "", "instance created",
		fmt.Sprintf(`{"auto":%q}`, strings.Join(prepared.Auto, ",")))
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetInstance(r.Context(), out.Name)
	if fresh != nil {
		out = *fresh
	}
	resp := pkgapi.InstanceCreateResponse{
		Instance:   s.toAPIInstance(out),
		AutoFilled: prepared.Auto,
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) buildInstanceContext(r *http.Request) (instance.Context, error) {
	list, err := s.store.ListInstances(r.Context())
	if err != nil {
		return instance.Context{}, err
	}
	names := map[string]struct{}{}
	ports := map[int]struct{}{}
	var nets []string
	for _, i := range list {
		names[i.Name] = struct{}{}
		if i.Port > 0 {
			ports[i.Port] = struct{}{}
		}
		if i.ServerNetwork != "" {
			nets = append(nets, i.ServerNetwork)
		}
	}
	bins, err := s.store.ListBinaries(r.Context())
	if err != nil {
		return instance.Context{}, err
	}
	binNames := map[string]struct{}{}
	for _, b := range bins {
		binNames[b.Name] = struct{}{}
	}
	// also allow config-registered binaries not yet in DB
	for name := range s.cfg.OpenVPN.Binaries {
		binNames[name] = struct{}{}
	}
	cas, _ := s.store.ListCAs(r.Context())
	defaultCA := ""
	if len(cas) > 0 {
		defaultCA = cas[0].Name
	}
	return instance.Context{
		ExistingNames: names,
		UsedPorts:     ports,
		UsedNetworks:  nets,
		DefaultBinary: s.cfg.OpenVPN.DefaultBinary,
		BinaryNames:   binNames,
		HasCA:         len(cas) > 0,
		DefaultCA:     defaultCA,
	}, nil
}

func createInputFromAPI(req pkgapi.InstanceCreateRequest) (instance.CreateInput, error) {
	remotes := toDBRemotes(req.Remotes)
	if req.Remote != "" {
		parsed, err := instance.ParseRemoteCSV(req.Remote)
		if err != nil {
			return instance.CreateInput{}, err
		}
		remotes = append(remotes, parsed...)
	}
	in := instance.CreateInput{
		Name: req.Name, Role: req.Role, Enabled: req.Enabled,
		BinaryName: req.BinaryName, BinaryPath: req.BinaryPath,
		DevType: req.DevType, Device: req.Device, Proto: req.Proto,
		LocalBind: req.LocalBind, Port: req.Port, Remotes: remotes,
		ServerNetwork: req.ServerNetwork, Topology: req.Topology,
		PoolStart: req.PoolStart, PoolEnd: req.PoolEnd,
		AuthMode: req.AuthMode, Cipher: req.Cipher, DataCiphers: req.DataCiphers, AuthDigest: req.AuthDigest,
		PushRoutes: req.PushRoutes, PushDNS: req.PushDNS, PushDomain: req.PushDomain,
		RedirectGateway: req.RedirectGateway,
		PKICaPath: req.PKICaPath, PKICertPath: req.PKICertPath, PKIKeyPath: req.PKIKeyPath,
		PKITLSCrypt: req.PKITLSCryptPath, PKIDHPath: req.PKIDHPath, StaticKeyPath: req.StaticKeyPath,
		ExtraDirectives: req.ExtraDirectives,
		Plugins: toDBPlugins(req.Plugins), EnvVars: toDBEnv(req.EnvVars), FeatureSets: req.FeatureSets,
		PreUp: req.PreUp, PostUp: req.PostUp, PreDown: req.PreDown, PostDown: req.PostDown,
		PublicEndpoint: req.PublicEndpoint,
		CAName: req.CAName, ServerCN: req.ServerCN, CreateCAIfEmpty: req.CreateCAIfEmpty,
	}
	if req.IssueServerCert != nil {
		in.IssueServerCert = *req.IssueServerCert
	}
	if req.GenerateTLSCrypt != nil {
		in.GenerateTLSCrypt = *req.GenerateTLSCrypt
	}
	return in, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
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
	if req.Plugins != nil {
		inst.Plugins = toDBPlugins(req.Plugins)
	}
	if req.EnvVars != nil {
		inst.EnvVars = toDBEnv(req.EnvVars)
	}
	if req.FeatureSets != nil {
		inst.FeatureSets = req.FeatureSets
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
	_ = s.backend.StopInstance(r.Context(), name)
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
	customPresets, _ := s.store.ListFeaturePresets(r.Context())
	res, err := confgen.RenderInstanceOpts(*inst, paths, clients, confgen.RenderOptions{CustomPresets: customPresets})
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
		Plugins: toAPIPlugins(i.Plugins), EnvVars: toAPIEnv(i.EnvVars), FeatureSets: i.FeatureSets,
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

func toDBPlugins(in []pkgapi.Plugin) []db.Plugin {
	if in == nil {
		return nil
	}
	out := make([]db.Plugin, 0, len(in))
	for _, p := range in {
		out = append(out, db.Plugin{Path: p.Path, Args: p.Args})
	}
	return out
}

func toAPIPlugins(in []db.Plugin) []pkgapi.Plugin {
	if in == nil {
		return nil
	}
	out := make([]pkgapi.Plugin, 0, len(in))
	for _, p := range in {
		out = append(out, pkgapi.Plugin{Path: p.Path, Args: p.Args})
	}
	return out
}

func toDBEnv(in []pkgapi.EnvVar) []db.EnvVar {
	if in == nil {
		return nil
	}
	out := make([]db.EnvVar, 0, len(in))
	for _, e := range in {
		out = append(out, db.EnvVar{Name: e.Name, Value: e.Value})
	}
	return out
}

func toAPIEnv(in []db.EnvVar) []pkgapi.EnvVar {
	if in == nil {
		return nil
	}
	out := make([]pkgapi.EnvVar, 0, len(in))
	for _, e := range in {
		out = append(out, pkgapi.EnvVar{Name: e.Name, Value: e.Value})
	}
	return out
}

package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/adopt"
	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/confimport"
	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/instance"
	"github.com/reloadlife/openvpnd/internal/netutil"
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

	out, prepared, err := s.createInstanceFromRequest(r, req)
	if err != nil {
		writeCreateError(w, err)
		return
	}
	resp := pkgapi.InstanceCreateResponse{
		Instance:   s.toAPIInstance(out),
		AutoFilled: prepared.Auto,
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleImportInstance(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.ImportInstanceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "content is required")
		return
	}

	parsed, err := confimport.Parse(req.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse_error", err.Error())
		return
	}

	// Optional role override when conf is ambiguous.
	if req.Role != "" {
		parsed.Role = strings.ToLower(strings.TrimSpace(req.Role))
	}

	// create defaults to true when omitted (adopt-friendly).
	doCreate := true
	if req.Create != nil {
		doCreate = *req.Create
	}

	// When creating, materialize inline PEM blocks under pki_dir/imported/<name>/.
	if doCreate && parsed.HasInline() {
		name, err := s.resolveImportName(r, req.Name)
		if err != nil {
			writeCreateError(w, err)
			return
		}
		if strings.TrimSpace(s.cfg.OpenVPN.PKIDir) == "" {
			writeError(w, http.StatusBadRequest, "pki_error",
				"openvpn.pki_dir is required to materialize inline PEM blocks on import")
			return
		}
		dest := filepath.Join(s.cfg.OpenVPN.PKIDir, "imported", name)
		if err := parsed.Materialize(confimport.MaterializeOptions{DestDir: dest}); err != nil {
			writeError(w, http.StatusInternalServerError, "materialize_error", err.Error())
			return
		}
		// Ensure create uses the resolved name so paths and instance match.
		if strings.TrimSpace(req.Name) == "" {
			req.Name = name
		}
	}

	createReq := applyImportOverrides(parsed.ToCreateRequest(), req)

	resp := pkgapi.ImportInstanceResponse{
		Parsed:   createReq,
		Warnings: append([]string(nil), parsed.Warnings...),
	}

	if !doCreate {
		resp.Created = false
		writeJSON(w, http.StatusOK, resp)
		return
	}

	out, prepared, err := s.createInstanceFromRequest(r, createReq)
	if err != nil {
		writeCreateError(w, err)
		return
	}
	inst := s.toAPIInstance(out)
	resp.Instance = &inst
	resp.AutoFilled = prepared.Auto
	resp.Created = true
	// Refresh parsed paths in response after materialize (createReq already has them).
	resp.Parsed = createReq
	resp.Warnings = append([]string(nil), parsed.Warnings...)
	_ = s.store.AddEvent(r.Context(), "info", "import", out.Name, "", "instance imported from conf",
		fmt.Sprintf(`{"auto":%q,"source":%q}`, strings.Join(prepared.Auto, ","), req.SourcePath))
	writeJSON(w, http.StatusCreated, resp)
}

// applyImportOverrides folds ImportInstanceRequest fields onto a create request.
func applyImportOverrides(createReq pkgapi.InstanceCreateRequest, req pkgapi.ImportInstanceRequest) pkgapi.InstanceCreateRequest {
	if req.Name != "" {
		createReq.Name = req.Name
	}
	if req.Enabled != nil {
		createReq.Enabled = req.Enabled
	}
	if req.BinaryName != "" {
		createReq.BinaryName = req.BinaryName
	}
	if req.PublicEndpoint != "" {
		createReq.PublicEndpoint = req.PublicEndpoint
	}
	if req.SourcePath != "" {
		// Preserve source path as a comment in extra directives for operators.
		note := "# imported from " + req.SourcePath + "\n"
		createReq.ExtraDirectives = note + createReq.ExtraDirectives
	}
	return createReq
}

// resolveImportName picks the instance name used for materialize dest and create.
// Mirrors instance.Prepare naming when name is empty/auto.
func (s *Server) resolveImportName(r *http.Request, requested string) (string, error) {
	name := strings.TrimSpace(requested)
	if name != "" && !netutil.IsAutoToken(name) {
		if err := instance.ValidateName(name); err != nil {
			return "", &createError{http.StatusBadRequest, "validation_error", err.Error()}
		}
		return name, nil
	}
	ctxBuild, err := s.buildInstanceContext(r)
	if err != nil {
		return "", err
	}
	// Match Prepare: next free ovpnN.
	for i := 0; i < 1000; i++ {
		n := fmt.Sprintf("ovpn%d", i)
		if _, taken := ctxBuild.ExistingNames[n]; !taken {
			return n, nil
		}
	}
	return fmt.Sprintf("ovpn-%d", len(ctxBuild.ExistingNames)), nil
}

func (s *Server) handleDiscoverOpenVPN(w http.ResponseWriter, r *http.Request) {
	cands, err := adopt.DiscoverOpenVPN()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "discover_error", err.Error())
		return
	}
	out := make([]pkgapi.OpenVPNCandidate, 0, len(cands))
	for _, c := range cands {
		out = append(out, pkgapi.OpenVPNCandidate{
			PID: c.PID, ConfPath: c.ConfPath, Cmdline: c.Cmdline, Binary: c.Binary,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAdoptInstance(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.AdoptInstanceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	confPath := strings.TrimSpace(req.ConfPath)
	if confPath == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "conf_path is required")
		return
	}
	if !filepath.IsAbs(confPath) {
		writeError(w, http.StatusBadRequest, "bad_request", "conf_path must be an absolute path on the daemon host")
		return
	}

	result, err := adopt.AdoptFromConf(confPath, req.Name)
	if err != nil {
		// Distinguish missing file vs parse/validation.
		msg := err.Error()
		status := http.StatusBadRequest
		code := "adopt_error"
		if strings.Contains(msg, "read conf:") {
			status = http.StatusNotFound
			code = "conf_not_found"
		}
		writeError(w, status, code, msg)
		return
	}

	// Materialize inline PEMs (same as import) so client/server certs are available.
	instName := strings.TrimSpace(req.Name)
	if instName == "" {
		instName = strings.TrimSpace(result.Request.Name)
	}
	if result.Parsed.HasInline() {
		if name, err := s.resolveImportName(r, instName); err == nil {
			instName = name
			if strings.TrimSpace(s.cfg.OpenVPN.PKIDir) != "" {
				dest := filepath.Join(s.cfg.OpenVPN.PKIDir, "imported", name)
				if err := result.Parsed.Materialize(confimport.MaterializeOptions{DestDir: dest}); err != nil {
					writeError(w, http.StatusInternalServerError, "materialize_error", err.Error())
					return
				}
				// Rebuild create request with materialized paths.
				result.Request = result.Parsed.ToCreateRequest()
				result.Request.Name = name
				result.Request.ExtraDirectives = "# adopted from " + confPath + "\n" + result.Request.ExtraDirectives
				result.Warnings = append([]string(nil), result.Parsed.Warnings...)
			}
		}
	}

	createReq := result.Request
	if req.Enabled != nil {
		createReq.Enabled = req.Enabled
	}
	if req.BinaryName != "" {
		createReq.BinaryName = req.BinaryName
	}
	// Optional absolute binary override from discover (e.g. /usr/bin/openvpn-linux).
	if bp := strings.TrimSpace(req.BinaryPath); bp != "" {
		createReq.BinaryPath = bp
		if createReq.BinaryName == "" {
			createReq.BinaryName = "adopted"
		}
		// Ensure registry entry so validation passes.
		if s.store != nil {
			_, _ = s.store.UpsertBinary(r.Context(), db.Binary{
				Name: createReq.BinaryName, Path: bp, Notes: "auto from adopt discover",
			})
		}
	}
	if req.PublicEndpoint != "" {
		createReq.PublicEndpoint = req.PublicEndpoint
	}

	// When take-over will force-enable after create, avoid a flapping start while
	// the foreign process still holds the port: create disabled first.
	takeoverLive := req.TakeOver && s.cfg != nil && s.cfg.OpenVPN.AdoptTakeoverEnabled
	if takeoverLive {
		disabled := false
		createReq.Enabled = &disabled
	}

	var notes []string
	notes = append(notes, "adopted conf from daemon host path "+confPath)
	if req.PID > 0 {
		notes = append(notes, fmt.Sprintf("operator-supplied pid=%d", req.PID))
	}

	out, prepared, err := s.createInstanceFromRequest(r, createReq)
	if err != nil {
		writeCreateError(w, err)
		return
	}

	if req.TakeOver {
		if !takeoverLive {
			// Legacy notes-only behavior when adopt_takeover_enabled=false.
			notes = append(notes,
				"take_over=true: openvpn.adopt_takeover_enabled=false — stop the existing openvpn process "+
					"(or disable its unit) so openvpnd can start a managed process; force-stop of foreign PIDs is disabled")
		} else {
			notes = append(notes, s.performAdoptTakeover(r, out.Name, req.PID)...)
		}
	} else {
		notes = append(notes,
			"take_over=false: instance is registered in desired state; if a foreign openvpn already holds the port, disable/stop it before enabling under openvpnd")
	}

	// Refresh instance after possible enable + reconcile.
	if fresh, err := s.store.GetInstance(r.Context(), out.Name); err == nil && fresh != nil {
		out = *fresh
	}
	inst := s.toAPIInstance(out)
	resp := pkgapi.AdoptInstanceResponse{
		Instance:   &inst,
		Parsed:     createReq,
		Warnings:   result.Warnings,
		AutoFilled: prepared.Auto,
		Notes:      notes,
		ConfPath:   confPath,
		PID:        req.PID,
	}
	_ = s.store.AddEvent(r.Context(), "info", "adopt", out.Name, "", "instance adopted from conf path",
		fmt.Sprintf(`{"auto":%q,"conf_path":%q,"take_over":%v,"pid":%d}`,
			strings.Join(prepared.Auto, ","), confPath, req.TakeOver, req.PID))
	writeJSON(w, http.StatusCreated, resp)
}

// performAdoptTakeover stops a verified openvpn PID (soft-fail), enables the
// instance, force-reconciles, and returns notes. Always safe to call after create.
func (s *Server) performAdoptTakeover(r *http.Request, name string, pid int) []string {
	var extra []string
	extra = append(extra, "take_over=true: attempting process takeover")

	if pid <= 0 {
		extra = append(extra, "take_over=true: no pid supplied; skipped process stop")
	} else {
		// Double-check identity before signal (also done inside StopProcess).
		info, err := adopt.InspectProcess(pid)
		if err != nil {
			extra = append(extra, fmt.Sprintf("take_over=true: refused/failed inspect pid %d: %v", pid, err))
			s.log.Warn("adopt_takeover inspect failed", "instance", name, "pid", pid, "err", err)
		} else if !info.SafeToStop {
			extra = append(extra, fmt.Sprintf("take_over=true: refused pid %d: %s", pid, info.Reason))
			s.log.Warn("adopt_takeover refused", "instance", name, "pid", pid, "reason", info.Reason)
		} else {
			if err := adopt.StopProcess(pid); err != nil {
				extra = append(extra, fmt.Sprintf("take_over=true: stop pid %d failed: %v", pid, err))
				s.log.Warn("adopt_takeover stop failed", "instance", name, "pid", pid, "err", err)
			} else {
				extra = append(extra, fmt.Sprintf("sent SIGTERM to pid %d", pid))
				s.log.Info("adopt_takeover stopped process", "instance", name, "pid", pid)
			}
		}
	}

	// Enable instance so reconciler starts a managed openvpn.
	if err := s.store.SetInstanceEnabled(r.Context(), name, true); err != nil {
		extra = append(extra, "take_over=true: enable instance failed: "+err.Error())
		s.log.Warn("adopt_takeover enable failed", "instance", name, "err", err)
	} else {
		extra = append(extra, "take_over=true: instance enabled")
	}
	if err := s.ForceReconcile(r.Context()); err != nil {
		extra = append(extra, "take_over=true: force reconcile failed: "+err.Error())
		s.log.Warn("adopt_takeover reconcile failed", "instance", name, "err", err)
	} else {
		extra = append(extra, "take_over=true: force reconcile completed")
	}

	_ = s.store.AddEvent(r.Context(), "info", "adopt_takeover", name, "",
		"adopt take-over of foreign openvpn process",
		fmt.Sprintf(`{"pid":%d,"notes":%q}`, pid, strings.Join(extra, "; ")))

	return extra
}

// createError carries HTTP status for create/import failures.
type createError struct {
	status int
	code   string
	msg    string
}

func (e *createError) Error() string { return e.msg }

func writeCreateError(w http.ResponseWriter, err error) {
	if ce, ok := err.(*createError); ok {
		writeError(w, ce.status, ce.code, ce.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}

// createInstanceFromRequest runs Prepare + store + optional PKI issue (shared by create and import).
func (s *Server) createInstanceFromRequest(r *http.Request, req pkgapi.InstanceCreateRequest) (db.Instance, instance.Result, error) {
	ctxBuild, err := s.buildInstanceContext(r)
	if err != nil {
		return db.Instance{}, instance.Result{}, &createError{http.StatusInternalServerError, "db_error", err.Error()}
	}

	in, err := createInputFromAPI(req)
	if err != nil {
		return db.Instance{}, instance.Result{}, &createError{http.StatusBadRequest, "bad_request", err.Error()}
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
		return db.Instance{}, instance.Result{}, &createError{http.StatusBadRequest, "validation_error", err.Error()}
	}

	out, err := s.store.CreateInstance(r.Context(), prepared.Instance)
	if err != nil {
		return db.Instance{}, instance.Result{}, &createError{http.StatusConflict, "create_failed", err.Error()}
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
					return db.Instance{}, instance.Result{}, &createError{http.StatusInternalServerError, "pki_error", err.Error()}
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
						return db.Instance{}, instance.Result{}, &createError{http.StatusBadRequest, "pki_error", "create CA: " + err.Error()}
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
			return db.Instance{}, instance.Result{}, &createError{http.StatusBadRequest, "pki_error", "instance created but cert issue failed: " + err.Error()}
		}
		ca, _ := s.store.GetCA(r.Context(), caName)
		fresh, _ := s.store.GetInstance(r.Context(), out.Name)
		if fresh != nil && ca != nil {
			fresh.PKICaPath = ca.CertPath
			fresh.PKICertPath = issued.CertPath
			fresh.PKIKeyPath = issued.KeyPath
			if ca.CRLPath != "" {
				fresh.PKICRLPath = ca.CRLPath
			}
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
	return out, prepared, nil
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
		PKICaPath:       req.PKICaPath, PKICertPath: req.PKICertPath, PKIKeyPath: req.PKIKeyPath,
		PKITLSCrypt: req.PKITLSCryptPath, PKIDHPath: req.PKIDHPath, StaticKeyPath: req.StaticKeyPath,
		ExtraDirectives: req.ExtraDirectives,
		Plugins:         toDBPlugins(req.Plugins), EnvVars: toDBEnv(req.EnvVars), FeatureSets: req.FeatureSets,
		PreUp: req.PreUp, PostUp: req.PostUp, PreDown: req.PreDown, PostDown: req.PostDown,
		PublicEndpoint: req.PublicEndpoint,
		MaxClients:     req.MaxClients, TLSVersionMin: req.TLSVersionMin,
		TunMTU: req.TunMTU, Sndbuf: req.Sndbuf, Rcvbuf: req.Rcvbuf,
		ServerIPv6: req.ServerIPv6, AuthUserPass: req.AuthUserPass,
		BridgeMode: req.BridgeMode, BridgeGateway: req.BridgeGateway,
		BridgePoolStart: req.BridgePoolStart, BridgePoolEnd: req.BridgePoolEnd, BridgeNetmask: req.BridgeNetmask,
		TLSCipher: req.TLSCipher, TLSCiphersuites: req.TLSCiphersuites,
		TLSGroups: req.TLSGroups, TLSCertProfile: req.TLSCertProfile,
		AuthUserPassVerify: req.AuthUserPassVerify, ScriptSecurity: req.ScriptSecurity,
		UsernameAsCommonName: req.UsernameAsCommonName,
		AuthUserPassFile:     req.AuthUserPassFile,
		IfconfigIPv6:         req.IfconfigIPv6,
		CAName:               req.CAName, ServerCN: req.ServerCN, CreateCAIfEmpty: req.CreateCAIfEmpty,
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
	if req.MaxClients != nil {
		inst.MaxClients = *req.MaxClients
	}
	if req.TLSVersionMin != nil {
		inst.TLSVersionMin = *req.TLSVersionMin
	}
	if req.TunMTU != nil {
		inst.TunMTU = *req.TunMTU
	}
	if req.Sndbuf != nil {
		inst.Sndbuf = *req.Sndbuf
	}
	if req.Rcvbuf != nil {
		inst.Rcvbuf = *req.Rcvbuf
	}
	if req.ServerIPv6 != nil {
		inst.ServerIPv6 = *req.ServerIPv6
	}
	if req.AuthUserPass != nil {
		inst.AuthUserPass = *req.AuthUserPass
	}
	if req.BridgeMode != nil {
		inst.BridgeMode = *req.BridgeMode
	}
	if req.BridgeGateway != nil {
		inst.BridgeGateway = *req.BridgeGateway
	}
	if req.BridgePoolStart != nil {
		inst.BridgePoolStart = *req.BridgePoolStart
	}
	if req.BridgePoolEnd != nil {
		inst.BridgePoolEnd = *req.BridgePoolEnd
	}
	if req.BridgeNetmask != nil {
		inst.BridgeNetmask = *req.BridgeNetmask
	}
	if req.TLSCipher != nil {
		inst.TLSCipher = *req.TLSCipher
	}
	if req.TLSCiphersuites != nil {
		inst.TLSCiphersuites = *req.TLSCiphersuites
	}
	if req.TLSGroups != nil {
		inst.TLSGroups = *req.TLSGroups
	}
	if req.TLSCertProfile != nil {
		inst.TLSCertProfile = *req.TLSCertProfile
	}
	if req.AuthUserPassVerify != nil {
		inst.AuthUserPassVerify = *req.AuthUserPassVerify
	}
	if req.ScriptSecurity != nil {
		inst.ScriptSecurity = *req.ScriptSecurity
	}
	if req.UsernameAsCommonName != nil {
		inst.UsernameAsCommonName = *req.UsernameAsCommonName
	}
	if req.AuthUserPassFile != nil {
		inst.AuthUserPassFile = *req.AuthUserPassFile
	}
	if req.IfconfigIPv6 != nil {
		inst.IfconfigIPv6 = *req.IfconfigIPv6
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

// Allowed OpenVPN management commands (whitelist). Never pass arbitrary shell.
var mgmtAllowedCommands = map[string]struct{}{
	"status":    {},
	"kill":      {},
	"signal":    {},
	"hold":      {},
	"log":       {},
	"state":     {},
	"bytecount": {},
	"pid":       {},
	"version":   {},
}

// OpenVPN management signals commonly used for soft restart / reload / exit.
var mgmtAllowedSignals = map[string]struct{}{
	"SIGUSR1": {},
	"SIGHUP":  {},
	"SIGTERM": {},
	"SIGUSR2": {},
	"SIGINT":  {},
}

func (s *Server) handleInstanceStatus(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	inst, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	out := pkgapi.InstanceStatus{
		Name: inst.Name,
		PID:  inst.PID,
	}
	if s.cache != nil {
		if st, ok := s.cache.GetInstance(name); ok {
			out.Up = st.Up
			if st.PID > 0 {
				out.PID = st.PID
			}
			out.RxBytes = st.RxBytes
			out.TxBytes = st.TxBytes
			out.ConnectedClients = st.ConnectedClients
			out.UpdatedAt = st.UpdatedAt
		}
	}

	mgmt, err := s.backend.Management(r.Context(), name)
	if err != nil {
		// Instance may be down; return structured snapshot from cache/DB rather than hard fail.
		if !out.Up {
			out.Error = "instance not running or management unavailable: " + err.Error()
			writeJSON(w, http.StatusOK, out)
			return
		}
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer func() { _ = mgmt.Close() }()

	live, err := mgmt.Status(r.Context())
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Up = true
	out.RxBytes = live.RxBytes
	out.TxBytes = live.TxBytes
	out.ConnectedClients = len(live.Clients)
	out.UpdatedAt = live.UpdatedAt
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	}
	if len(live.Clients) > 0 {
		out.Clients = make([]pkgapi.InstanceStatusClient, 0, len(live.Clients))
		for _, c := range live.Clients {
			out.Clients = append(out.Clients, pkgapi.InstanceStatusClient{
				CommonName:     c.CommonName,
				RealAddress:    c.RealAddress,
				VirtualAddress: c.VirtualAddress,
				ConnectedSince: c.ConnectedSince,
				RxBytes:        c.RxBytes,
				TxBytes:        c.TxBytes,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleInstanceMgmt(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if _, err := s.store.GetInstance(r.Context(), name); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	var req pkgapi.MgmtCommandRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	cmd, err := buildMgmtCommand(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	mgmt, err := s.backend.Management(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusConflict, "not_running", "management unavailable: "+err.Error())
		return
	}
	defer func() { _ = mgmt.Close() }()

	output, err := mgmt.Raw(r.Context(), cmd)
	if err != nil {
		// Return partial output with error when management replied ERROR:
		if output != "" {
			writeError(w, http.StatusBadGateway, "mgmt_error", err.Error()+"; output="+output)
			return
		}
		writeError(w, http.StatusBadGateway, "mgmt_error", err.Error())
		return
	}

	_ = s.store.AddEvent(r.Context(), "info", "mgmt", name, "", "management command",
		fmt.Sprintf(`{"command":%q}`, cmd))
	writeJSON(w, http.StatusOK, pkgapi.MgmtCommandResponse{Output: output})
}

// buildMgmtCommand validates the whitelist and assembles a single management line.
func buildMgmtCommand(req pkgapi.MgmtCommandRequest) (string, error) {
	verb := strings.ToLower(strings.TrimSpace(req.Command))
	if verb == "" {
		return "", fmt.Errorf("command is required")
	}
	if _, ok := mgmtAllowedCommands[verb]; !ok {
		return "", fmt.Errorf("command %q not allowed; whitelist: status, kill, signal, hold, log, state, bytecount, pid, version", req.Command)
	}
	for i, a := range req.Args {
		if strings.ContainsAny(a, "\r\n") {
			return "", fmt.Errorf("args[%d] must not contain newlines", i)
		}
		if strings.TrimSpace(a) == "" {
			return "", fmt.Errorf("args[%d] is empty", i)
		}
	}

	switch verb {
	case "kill":
		if len(req.Args) < 1 {
			return "", fmt.Errorf("kill requires args[0] = common name or IP:port")
		}
		// OpenVPN kill takes a single CN or IP:port token.
		return "kill " + strings.TrimSpace(req.Args[0]), nil
	case "signal":
		if len(req.Args) < 1 {
			return "", fmt.Errorf("signal requires args[0] = SIGUSR1|SIGHUP|SIGTERM|SIGUSR2|SIGINT")
		}
		sig := strings.ToUpper(strings.TrimSpace(req.Args[0]))
		if _, ok := mgmtAllowedSignals[sig]; !ok {
			return "", fmt.Errorf("signal %q not allowed; use SIGUSR1, SIGHUP, SIGTERM, SIGUSR2, or SIGINT", req.Args[0])
		}
		return "signal " + sig, nil
	default:
		if len(req.Args) == 0 {
			return verb, nil
		}
		parts := make([]string, 0, 1+len(req.Args))
		parts = append(parts, verb)
		for _, a := range req.Args {
			parts = append(parts, strings.TrimSpace(a))
		}
		return strings.Join(parts, " "), nil
	}
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
		Remotes:       toAPIRemotes(i.Remotes),
		ServerNetwork: i.ServerNetwork, Topology: i.Topology,
		PoolStart: i.PoolStart, PoolEnd: i.PoolEnd,
		AuthMode: i.AuthMode, Cipher: i.Cipher, DataCiphers: i.DataCiphers, AuthDigest: i.AuthDigest,
		PushRoutes: i.PushRoutes, PushDNS: i.PushDNS, PushDomain: i.PushDomain,
		RedirectGateway: i.RedirectGateway,
		PKICaPath:       i.PKICaPath, PKICertPath: i.PKICertPath, PKIKeyPath: i.PKIKeyPath,
		PKITLSCryptPath: i.PKITLSCryptPath, PKIDHPath: i.PKIDHPath, PKICRLPath: i.PKICRLPath,
		StaticKeyPath:   i.StaticKeyPath,
		ExtraDirectives: i.ExtraDirectives,
		Plugins:         toAPIPlugins(i.Plugins), EnvVars: toAPIEnv(i.EnvVars), FeatureSets: i.FeatureSets,
		PreUp: i.PreUp, PostUp: i.PostUp, PreDown: i.PreDown, PostDown: i.PostDown,
		MaxClients: i.MaxClients, TLSVersionMin: i.TLSVersionMin, TunMTU: i.TunMTU,
		Sndbuf: i.Sndbuf, Rcvbuf: i.Rcvbuf, ServerIPv6: i.ServerIPv6, AuthUserPass: i.AuthUserPass,
		BridgeMode: i.BridgeMode, BridgeGateway: i.BridgeGateway,
		BridgePoolStart: i.BridgePoolStart, BridgePoolEnd: i.BridgePoolEnd, BridgeNetmask: i.BridgeNetmask,
		TLSCipher: i.TLSCipher, TLSCiphersuites: i.TLSCiphersuites,
		TLSGroups: i.TLSGroups, TLSCertProfile: i.TLSCertProfile,
		AuthUserPassVerify: i.AuthUserPassVerify, ScriptSecurity: i.ScriptSecurity,
		UsernameAsCommonName: i.UsernameAsCommonName,
		AuthUserPassFile:     i.AuthUserPassFile,
		IfconfigIPv6:         i.IfconfigIPv6,
		PublicEndpoint:       i.PublicEndpoint,
		PID:                  i.PID, LastError: i.LastError, ConnectedClients: i.ConnectedClients,
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

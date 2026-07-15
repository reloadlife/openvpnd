package api

import (
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/pki"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) pkiManager() (*pki.Manager, error) {
	return pki.NewManager(s.cfg.OpenVPN.PKIDir)
}

func (s *Server) handleListCAs(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListCAs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.CA, 0, len(list))
	for _, c := range list {
		out = append(out, toAPICA(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateCA(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.CreateCARequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.CommonName) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "common_name required")
		return
	}
	mgr, err := s.pkiManager()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pki_error", err.Error())
		return
	}
	mat, err := mgr.CreateCA(pki.CreateCAOptions{
		Name: req.Name, CommonName: req.CommonName, Org: req.Org,
		ValidDays: req.ValidDays, KeyType: req.KeyType, RSABits: req.RSABits,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "create_failed", err.Error())
		return
	}
	ca, err := s.store.UpsertCA(r.Context(), db.CA{
		Name: mat.Name, CommonName: mat.CN, Org: req.Org,
		CertPath: mat.CertPath, KeyPath: mat.KeyPath,
		NotAfter: mat.NotAfter.UTC().Format(time.RFC3339), SerialNext: mat.Serial,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	_ = s.store.AddEvent(r.Context(), "info", "pki", "", "", "CA created: "+ca.Name, "{}")
	writeJSON(w, http.StatusCreated, toAPICA(ca))
}

func (s *Server) handleGetCA(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ca, err := s.store.GetCA(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAPICA(*ca))
}

func (s *Server) handleDeleteCA(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.store.DeleteCA(r.Context(), name); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListCerts(w http.ResponseWriter, r *http.Request) {
	ca := r.URL.Query().Get("ca")
	list, err := s.store.ListCertificates(r.Context(), ca)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.Certificate, 0, len(list))
	for _, c := range list {
		out = append(out, toAPICert(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleIssueCert(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.IssueCertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.CAName == "" || req.CommonName == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "ca_name and common_name required")
		return
	}
	kind := strings.ToLower(req.Kind)
	if kind == "" {
		kind = "client"
	}
	issued, rec, err := s.issueAndStore(r, req.CAName, kind, req.CommonName, req.ValidDays, req.DNSNames, req.IPs, req.KeyType, req.InstanceName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "issue_failed", err.Error())
		return
	}
	_ = issued
	writeJSON(w, http.StatusCreated, toAPICert(rec))
}

func (s *Server) handleGetCert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	c, err := s.store.GetCertificate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAPICert(*c))
}

func (s *Server) handleGenerateTLSCrypt(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.TLSCryptRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	if req.Name == "" {
		req.Name = "default"
	}
	mgr, err := s.pkiManager()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pki_error", err.Error())
		return
	}
	path, err := mgr.GenerateTLSCrypt(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pki_error", err.Error())
		return
	}
	k, err := s.store.UpsertTLSCrypt(r.Context(), req.Name, path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pkgapi.TLSCryptKey{Name: k.Name, Path: k.Path, CreatedAt: k.CreatedAt})
}

func (s *Server) handleListTLSCrypt(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTLSCrypt(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.TLSCryptKey, 0, len(list))
	for _, k := range list {
		out = append(out, pkgapi.TLSCryptKey{Name: k.Name, Path: k.Path, CreatedAt: k.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/instances/{name}/issue-server-cert
func (s *Server) handleIssueServerCert(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	inst, err := s.store.GetInstance(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var req pkgapi.IssueServerCertRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	caName := req.CAName
	if caName == "" {
		cas, _ := s.store.ListCAs(r.Context())
		if len(cas) == 0 {
			writeError(w, http.StatusBadRequest, "no_ca", "create a CA first (POST /v1/pki/cas)")
			return
		}
		caName = cas[0].Name
	}
	cn := req.CommonName
	if cn == "" {
		if inst.PublicEndpoint != "" {
			cn = strings.Split(inst.PublicEndpoint, ":")[0]
		} else {
			cn = name
		}
	}
	dns := req.DNSNames
	if len(dns) == 0 {
		dns = []string{cn}
	}
	issued, rec, err := s.issueAndStore(r, caName, "server", cn, req.ValidDays, dns, req.IPs, req.KeyType, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "issue_failed", err.Error())
		return
	}
	// wire instance PKI paths
	ca, err := s.store.GetCA(r.Context(), caName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	inst.PKICaPath = ca.CertPath
	inst.PKICertPath = issued.CertPath
	inst.PKIKeyPath = issued.KeyPath
	if ca.CRLPath != "" {
		inst.PKICRLPath = ca.CRLPath
	}
	if req.TLSCrypt != "" {
		tc, err := s.store.GetTLSCrypt(r.Context(), req.TLSCrypt)
		if err == nil {
			inst.PKITLSCryptPath = tc.Path
		}
	} else if req.GenerateTLSCrypt {
		mgr, _ := s.pkiManager()
		if mgr != nil {
			if path, err := mgr.GenerateTLSCrypt(name); err == nil {
				_, _ = s.store.UpsertTLSCrypt(r.Context(), name, path)
				inst.PKITLSCryptPath = path
			}
		}
	}
	if _, err := s.store.UpdateInstance(r.Context(), *inst); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetInstance(r.Context(), name)
	writeJSON(w, http.StatusOK, map[string]any{
		"certificate": toAPICert(rec),
		"instance":    s.toAPIInstance(*fresh),
	})
}

// POST /v1/instances/{name}/clients/{cn}/issue-cert
func (s *Server) handleIssueClientCert(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	cli, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	var req pkgapi.IssueClientCertRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	caName := req.CAName
	if caName == "" {
		// prefer instance's CA if path matches a known CA
		inst, _ := s.store.GetInstance(r.Context(), name)
		cas, _ := s.store.ListCAs(r.Context())
		if inst != nil {
			for _, c := range cas {
				if c.CertPath == inst.PKICaPath {
					caName = c.Name
					break
				}
			}
		}
		if caName == "" && len(cas) > 0 {
			caName = cas[0].Name
		}
	}
	if caName == "" {
		writeError(w, http.StatusBadRequest, "no_ca", "create a CA first")
		return
	}
	issued, rec, err := s.issueAndStore(r, caName, "client", cn, req.ValidDays, nil, nil, req.KeyType, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "issue_failed", err.Error())
		return
	}
	cli.ClientCertPath = issued.CertPath
	cli.ClientKeyPath = issued.KeyPath
	cli.CertRef = fmt.Sprintf("%s/%d", caName, rec.ID)
	out, err := s.store.UpdateClient(r.Context(), name, cn, *cli)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"certificate": toAPICert(rec),
		"client":      s.toAPIClient(out),
	})
}

func (s *Server) handleRevokeCert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req pkgapi.RevokeCertRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	c, err := s.store.GetCertificate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if c.Revoked {
		writeJSON(w, http.StatusOK, toAPICert(*c))
		return
	}
	if err := s.store.RevokeCertificate(r.Context(), id, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if _, err := s.rebuildCRLForCA(r, c.CAName); err != nil {
		writeError(w, http.StatusInternalServerError, "crl_error", err.Error())
		return
	}
	_ = s.store.AddEvent(r.Context(), "warn", "pki", c.InstanceName, c.CommonName, "certificate revoked", "{}")
	_ = s.ForceReconcile(r.Context())
	fresh, err := s.store.GetCertificate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAPICert(*fresh))
}

func (s *Server) handleRebuildCRL(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ca, err := s.rebuildCRLForCA(r, name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "crl_error", err.Error())
		return
	}
	_ = s.ForceReconcile(r.Context())
	writeJSON(w, http.StatusOK, toAPICA(*ca))
}

func (s *Server) handleRenewCert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req pkgapi.RenewCertRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	old, err := s.store.GetCertificate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	// If previously revoked, keep old serial on CRL; re-issue overwrites files and clears revoked.
	issued, rec, err := s.issueAndStore(r, old.CAName, old.Kind, old.CommonName, req.ValidDays, req.DNSNames, req.IPs, req.KeyType, old.InstanceName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "renew_failed", err.Error())
		return
	}
	_ = issued
	// ensure revoked cleared on the upserted row (same CN)
	if rec.Revoked {
		_ = s.store.ClearCertificateRevocation(r.Context(), rec.ID)
		fresh, err := s.store.GetCertificate(r.Context(), rec.ID)
		if err == nil {
			rec = *fresh
		}
	}
	// If the old cert was revoked, rebuild CRL so it still lists the old serial
	// (new serial is not revoked). ListRevoked only has currently-revoked rows —
	// after clear, old serial drops from CRL, which is correct for "renew replaces identity".
	// Re-issue path intentionally un-revokes the CN record.
	_ = s.store.AddEvent(r.Context(), "info", "pki", rec.InstanceName, rec.CommonName, "certificate renewed", "{}")
	writeJSON(w, http.StatusOK, toAPICert(rec))
}

func (s *Server) rebuildCRLForCA(r *http.Request, caName string) (*db.CA, error) {
	ca, err := s.store.GetCA(r.Context(), caName)
	if err != nil {
		return nil, err
	}
	mgr, err := s.pkiManager()
	if err != nil {
		return nil, err
	}
	revoked, err := s.store.ListRevokedCertificates(r.Context(), caName)
	if err != nil {
		return nil, err
	}
	entries := make([]pki.RevokedEntry, 0, len(revoked))
	for _, c := range revoked {
		if c.Serial <= 0 {
			continue
		}
		revAt := time.Now().UTC()
		if c.RevokedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, c.RevokedAt); err == nil {
				revAt = t
			} else if t, err := time.Parse(time.RFC3339, c.RevokedAt); err == nil {
				revAt = t
			}
		}
		entries = append(entries, pki.RevokedEntry{
			Serial:    big.NewInt(c.Serial),
			RevokedAt: revAt,
			Reason:    c.RevokeReason,
		})
	}
	crlPath, crlNumber, err := mgr.RebuildCRL(caName, entries)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateCACRL(r.Context(), caName, crlPath, crlNumber); err != nil {
		return nil, err
	}
	// attach CRL path to all server instances using this CA cert path
	_, _ = s.store.SetInstancesCRLPath(r.Context(), ca.CertPath, crlPath)
	out, err := s.store.GetCA(r.Context(), caName)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) issueAndStore(r *http.Request, caName, kind, cn string, days int, dns, ips []string, keyType, instance string) (pki.IssuedCert, db.Certificate, error) {
	if caName == "" {
		cas, err := s.store.ListCAs(r.Context())
		if err != nil || len(cas) == 0 {
			return pki.IssuedCert{}, db.Certificate{}, fmt.Errorf("no CA available — create one with POST /v1/pki/cas")
		}
		caName = cas[0].Name
	}
	// ensure CA in DB or on disk
	if _, err := s.store.GetCA(r.Context(), caName); err != nil {
		// try import from disk if files exist
		mgr, err2 := s.pkiManager()
		if err2 != nil {
			return pki.IssuedCert{}, db.Certificate{}, err
		}
		cp, kp, err2 := mgr.CAPaths(caName)
		if err2 != nil {
			return pki.IssuedCert{}, db.Certificate{}, fmt.Errorf("CA %q not found", caName)
		}
		_, _ = s.store.UpsertCA(r.Context(), db.CA{
			Name: caName, CommonName: caName, CertPath: cp, KeyPath: kp, SerialNext: 2,
		})
	}
	mgr, err := s.pkiManager()
	if err != nil {
		return pki.IssuedCert{}, db.Certificate{}, err
	}
	issued, err := mgr.Issue(pki.IssueOptions{
		CAName: caName, Kind: kind, CommonName: cn, ValidDays: days,
		DNSNames: dns, IPs: ips, KeyType: keyType,
	})
	if err != nil {
		return pki.IssuedCert{}, db.Certificate{}, err
	}
	rec, err := s.store.UpsertCertificate(r.Context(), db.Certificate{
		CAName: caName, Kind: kind, CommonName: cn,
		CertPath: issued.CertPath, KeyPath: issued.KeyPath,
		NotBefore: issued.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:  issued.NotAfter.UTC().Format(time.RFC3339),
		Serial:    issued.Serial, Fingerprint: issued.Fingerprint,
		InstanceName: instance,
		// renew / re-issue always clears revoked state on the CN record
		Revoked: false, RevokedAt: "", RevokeReason: "",
	})
	if err != nil {
		return pki.IssuedCert{}, db.Certificate{}, err
	}
	_ = s.store.AddEvent(r.Context(), "info", "pki", instance, cn, "issued "+kind+" cert", "{}")
	return issued, rec, nil
}

func toAPICA(c db.CA) pkgapi.CA {
	return pkgapi.CA{
		Name: c.Name, CommonName: c.CommonName, Org: c.Org,
		CertPath: c.CertPath, KeyPath: c.KeyPath,
		CRLPath: c.CRLPath, CRLNumber: c.CRLNumber,
		NotAfter: c.NotAfter,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toAPICert(c db.Certificate) pkgapi.Certificate {
	return pkgapi.Certificate{
		ID: c.ID, CAName: c.CAName, Kind: c.Kind, CommonName: c.CommonName,
		CertPath: c.CertPath, KeyPath: c.KeyPath,
		NotBefore: c.NotBefore, NotAfter: c.NotAfter,
		Serial: c.Serial, Fingerprint: c.Fingerprint, Revoked: c.Revoked,
		RevokedAt: c.RevokedAt, RevokeReason: c.RevokeReason,
		InstanceName: c.InstanceName, Notes: c.Notes, CreatedAt: c.CreatedAt,
	}
}

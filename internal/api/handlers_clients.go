package api

import (
	"net/http"

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
	if req.CommonName == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "common_name required")
		return
	}
	staticIP := req.StaticIP
	if netutil.IsAutoToken(staticIP) && inst.ServerNetwork != "" {
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
	} else if staticIP != "" {
		if err := netutil.ValidateIP(staticIP); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	c, err := s.store.CreateClient(r.Context(), name, db.Client{
		CommonName: req.CommonName, Name: req.Name, Notes: req.Notes,
		StaticIP: staticIP, PushRoutes: req.PushRoutes, Suspended: req.Suspended,
		TrafficLimitBytes: req.TrafficLimitBytes, BandwidthRxBps: req.BandwidthRxBps, BandwidthTxBps: req.BandwidthTxBps,
		CertRef: req.CertRef, ClientCertPath: req.ClientCertPath, ClientKeyPath: req.ClientKeyPath, Tags: req.Tags,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "create_failed", err.Error())
		return
	}
	_ = s.store.AddEvent(r.Context(), "info", "create", name, c.CommonName, "client created", "{}")
	_ = s.ForceReconcile(r.Context())
	fresh, _ := s.store.GetClient(r.Context(), name, c.CommonName)
	if fresh != nil {
		c = *fresh
	}
	writeJSON(w, http.StatusCreated, s.toAPIClient(c))
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
		StaticIP: c.StaticIP, PushRoutes: c.PushRoutes, Suspended: c.Suspended, Connected: connected,
		TrafficLimitBytes: c.TrafficLimitBytes, BandwidthRxBps: c.BandwidthRxBps, BandwidthTxBps: c.BandwidthTxBps,
		CertRef: c.CertRef, ClientCertPath: c.ClientCertPath, ClientKeyPath: c.ClientKeyPath,
		RealAddress: c.RealAddress, VirtualAddress: c.VirtualAddress,
		ConnectedSince: c.ConnectedSince,
		RxBytes: c.EffectiveRx(), TxBytes: c.EffectiveTx(),
		RxBps: c.LastRxBps, TxBps: c.LastTxBps, Tags: c.Tags,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

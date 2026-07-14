package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/db"
	"github.com/reloadlife/openvpnd/internal/features"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) handleListFeaturePresets(w http.ResponseWriter, r *http.Request) {
	custom, err := s.store.ListFeaturePresets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	merged := features.ListMerged(custom)
	out := make([]pkgapi.FeaturePreset, 0, len(merged))
	for _, p := range merged {
		out = append(out, toAPIFeature(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpsertFeaturePreset(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.FeaturePreset
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "id required")
		return
	}
	// refuse overwriting pure builtin IDs unless saving custom override
	p, err := s.store.UpsertFeaturePreset(r.Context(), db.FeaturePreset{
		ID: req.ID, Description: req.Description, ExtraDirectives: req.ExtraDirectives,
		Plugins: toDBPlugins(req.Plugins), EnvVars: toDBEnv(req.EnvVars), Notes: req.Notes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAPIFeature(p))
}

func (s *Server) handleDeleteFeaturePreset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteFeaturePreset(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPIFeature(p db.FeaturePreset) pkgapi.FeaturePreset {
	return pkgapi.FeaturePreset{
		ID: p.ID, Description: p.Description, ExtraDirectives: p.ExtraDirectives,
		Plugins: toAPIPlugins(p.Plugins), EnvVars: toAPIEnv(p.EnvVars),
		Notes: p.Notes, Builtin: p.Builtin,
	}
}

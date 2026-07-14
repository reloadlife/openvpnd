package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/db"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

func (s *Server) handleListBinaries(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListBinaries(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.Binary, 0, len(list))
	for _, b := range list {
		out = append(out, toAPIBinary(b))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateBinary(w http.ResponseWriter, r *http.Request) {
	var req pkgapi.BinaryCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Name == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name and path required")
		return
	}
	b, err := s.store.UpsertBinary(r.Context(), db.Binary{Name: req.Name, Path: req.Path, Notes: req.Notes})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	// probe version
	if ver, err := s.backend.ProbeBinary(r.Context(), b.Path); err == nil {
		_ = s.store.UpdateBinaryVersion(r.Context(), b.Name, ver)
		b.Version = ver
	}
	writeJSON(w, http.StatusCreated, toAPIBinary(b))
}

func (s *Server) handleGetBinary(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	b, err := s.store.GetBinary(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAPIBinary(*b))
}

func (s *Server) handleDeleteBinary(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.store.DeleteBinary(r.Context(), name); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPIBinary(b db.Binary) pkgapi.Binary {
	return pkgapi.Binary{
		Name: b.Name, Path: b.Path, Version: b.Version, Notes: b.Notes,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

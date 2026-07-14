package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/reloadlife/openvpnd/internal/confgen"
	"github.com/reloadlife/openvpnd/internal/db"
	pkgapi "github.com/reloadlife/openvpnd/pkg/api"
)

// handlePublicProfile serves a .ovpn via presigned token (no bearer auth).
// GET /p/{token}
func (s *Server) handlePublicProfile(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "token required")
		return
	}
	pt, err := s.store.ConsumeProfileToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", err.Error())
		return
	}
	body, filename, err := s.renderClientOVPN(r.Context(), pt.InstanceName, pt.CommonName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-openvpn-profile")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
	_ = s.store.AddEvent(r.Context(), "info", "profile_download", pt.InstanceName, pt.CommonName,
		"profile downloaded via token", fmt.Sprintf(`{"token_id":%d,"use_count":%d}`, pt.ID, pt.UseCount))
}

// handleClientConfig returns .ovpn with bearer auth (admin/API).
// GET /v1/instances/{name}/clients/{cn}/client-config
func (s *Server) handleClientConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	body, filename, err := s.renderClientOVPN(r.Context(), name, cn)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			code = http.StatusNotFound
		}
		writeError(w, code, "profile_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-openvpn-profile")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// handleCreateProfileLink mints a time-limited download + OpenVPN Connect import URL.
// POST /v1/instances/{name}/clients/{cn}/profile-link
func (s *Server) handleCreateProfileLink(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	cli, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	// Validate we can render before minting
	if _, _, err := s.renderClientOVPN(r.Context(), name, cn); err != nil {
		writeError(w, http.StatusBadRequest, "profile_error", err.Error())
		return
	}

	var req pkgapi.ProfileLinkRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}

	ttl := s.cfg.ProfileLinkTTL()
	if req.TTL != "" {
		d, err := time.ParseDuration(req.TTL)
		if err != nil || d <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid ttl")
			return
		}
		ttl = d
	}
	maxUses := s.cfg.ProfileLinks.DefaultMaxUses
	if req.MaxUses != nil {
		maxUses = *req.MaxUses
	}

	pt, err := s.store.CreateProfileToken(r.Context(), cli.ID, cli.InstanceID, ttl, maxUses, req.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	link := s.toProfileLink(pt)
	_ = s.store.AddEvent(r.Context(), "info", "profile_link", name, cn,
		"profile link created", fmt.Sprintf(`{"expires_at":%q,"max_uses":%d}`, pt.ExpiresAt.Format(time.RFC3339), maxUses))
	writeJSON(w, http.StatusCreated, link)
}

// handleListProfileLinks lists tokens for a client.
func (s *Server) handleListProfileLinks(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cn := chi.URLParam(r, "cn")
	cli, err := s.store.GetClient(r.Context(), name, cn)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	list, err := s.store.ListProfileTokensByClient(r.Context(), cli.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]pkgapi.ProfileLink, 0, len(list))
	for _, pt := range list {
		out = append(out, s.toProfileLink(pt))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevokeProfileLink revokes a token.
// DELETE /v1/profile-tokens/{token}
func (s *Server) handleRevokeProfileLink(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if err := s.store.RevokeProfileToken(r.Context(), token); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) toProfileLink(pt db.ProfileToken) pkgapi.ProfileLink {
	download := strings.TrimRight(s.cfg.PublicBase(), "/") + "/p/" + pt.Token
	// OpenVPN Connect deep link (Access Server style).
	importURL := "openvpn://import-profile/" + download
	return pkgapi.ProfileLink{
		Token:       pt.Token,
		DownloadURL: download,
		ImportURL:   importURL,
		ExpiresAt:   pt.ExpiresAt,
		MaxUses:     pt.MaxUses,
		UseCount:    pt.UseCount,
		Note:        pt.Note,
		Instance:    pt.InstanceName,
		CommonName:  pt.CommonName,
	}
}

func (s *Server) renderClientOVPN(ctx context.Context, instanceName, cn string) (body, filename string, err error) {
	inst, err := s.store.GetInstance(ctx, instanceName)
	if err != nil {
		return "", "", err
	}
	cli, err := s.store.GetClient(ctx, instanceName, cn)
	if err != nil {
		return "", "", err
	}
	mat, err := confgen.LoadMaterialFromPaths(inst.PKICaPath, cli.ClientCertPath, cli.ClientKeyPath, inst.PKITLSCryptPath)
	if err != nil {
		return "", "", err
	}
	out, err := confgen.RenderClientProfile(*inst, *cli, mat, confgen.ProfileOptions{Inline: true})
	if err != nil {
		return "", "", err
	}
	safeCN := confgen.SafeCNFilename(cn)
	filename = fmt.Sprintf("%s-%s.ovpn", instanceName, safeCN)
	return out, filename, nil
}

package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/reloadlife/openvpnd/internal/config"
)

type ctxKey int

const (
	roleCtxKey ctxKey = iota + 1
	tokenNameCtxKey
)

// RoleFromContext returns the authenticated API role (admin|operator|readonly).
func RoleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(roleCtxKey).(string); ok {
		return v
	}
	return ""
}

// TokenNameFromContext returns the configured name of the matched token.
func TokenNameFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tokenNameCtxKey).(string); ok {
		return v
	}
	return ""
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

// tokenAuth validates Bearer tokens and attaches role + token name to the request context.
func tokenAuth(principals []config.AuthPrincipal) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(principals) == 0 {
				writeError(w, http.StatusInternalServerError, "misconfigured", "auth token not configured")
				return
			}
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			got := strings.TrimPrefix(h, "Bearer ")
			match, ok := matchPrincipal(got, principals)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), roleCtxKey, match.Role)
			ctx = context.WithValue(ctx, tokenNameCtxKey, match.Name)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func matchPrincipal(got string, principals []config.AuthPrincipal) (config.AuthPrincipal, bool) {
	var matched config.AuthPrincipal
	found := 0
	for _, p := range principals {
		if len(got) != len(p.Token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(p.Token)) == 1 {
			matched = p
			found = 1
		}
	}
	return matched, found == 1
}

// roleGuard enforces daemon read-only mode and per-token RBAC.
//
//	readonly  — GET/HEAD/OPTIONS only
//	operator  — mutations allowed except DELETE /v1/pki/cas/* and restore/system destructive paths
//	admin     — full access
func roleGuard(daemonReadOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if daemonReadOnly {
				switch r.Method {
				case http.MethodGet, http.MethodHead, http.MethodOptions:
				default:
					writeError(w, http.StatusForbidden, "read_only", "daemon is in read-only mode")
					return
				}
			}

			role := RoleFromContext(r.Context())
			switch role {
			case config.RoleAdmin:
				// full access
			case config.RoleReadonly:
				switch r.Method {
				case http.MethodGet, http.MethodHead, http.MethodOptions:
				default:
					writeError(w, http.StatusForbidden, "forbidden", "readonly role cannot mutate")
					return
				}
			case config.RoleOperator:
				if operatorForbidden(r) {
					writeError(w, http.StatusForbidden, "forbidden", "operator role cannot perform this action")
					return
				}
			default:
				writeError(w, http.StatusForbidden, "forbidden", "unknown or missing role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// operatorForbidden blocks CA deletion and any restore / system-destructive routes.
func operatorForbidden(r *http.Request) bool {
	path := r.URL.Path
	// DELETE /v1/pki/cas/{name}
	if r.Method == http.MethodDelete && isPKICaPath(path) {
		return true
	}
	// Future / reserved: restore + system destructive APIs
	if strings.Contains(path, "/restore") {
		return true
	}
	if strings.HasPrefix(path, "/v1/system/") {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return false
		default:
			// POST /v1/system/backup is admin-only
			return true
		}
	}
	return false
}

func isPKICaPath(path string) bool {
	const prefix = "/v1/pki/cas/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" || strings.Contains(rest, "/") {
		// empty or sub-resource (e.g. rebuild-crl) — not a CA delete target
		return false
	}
	return true
}

// statusRecorder captures the status code without buffering the body.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// auditMutations records successful non-GET API mutations as kind=api events.
// Never stores Authorization headers or raw bearer tokens.
func (s *Server) auditMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 0}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		// Only successful mutations (2xx).
		if rec.status < 200 || rec.status >= 300 {
			return
		}
		if s.store == nil {
			return
		}
		role := RoleFromContext(r.Context())
		name := TokenNameFromContext(r.Context())
		metaObj := map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"status": rec.status,
		}
		if role != "" {
			metaObj["role"] = role
		}
		if name != "" {
			metaObj["token_name"] = name
		}
		if rid := w.Header().Get("X-Request-ID"); rid != "" {
			metaObj["request_id"] = rid
		}
		meta, _ := json.Marshal(metaObj)
		msg := fmt.Sprintf("%s %s → %d", r.Method, r.URL.Path, rec.status)
		_ = s.store.AddEvent(r.Context(), "info", "api", "", "", msg, string(meta))
	})
}
